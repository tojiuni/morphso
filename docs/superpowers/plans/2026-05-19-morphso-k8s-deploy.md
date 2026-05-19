# morphso K8s 배포 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** morphso-hub를 neunexus K8s morphso 네임스페이스에 배포하고, Woodpecker CI/CD + ArgoCD로 자동 배포 파이프라인을 구성한다. morphso CLI는 goreleaser로 멀티플랫폼 바이너리를 빌드해 GitHub Releases에 배포한다.

**Architecture:** 기존 neunexus 인프라(CNPG PostgreSQL in taxon ns, Redis in taxon ns, Vault + VSO, Traefik, Woodpecker CI, ArgoCD Image Updater)를 활용. 시크릿은 VSO(Vault Secrets Operator)로 `secret/neunexus/morphso` → K8s Secret `morphso-secrets` 동기화. 이미지는 Woodpecker CI가 `artifacts.toji.homes/morphso/morphso-hub:latest` 로 푸시하면 ArgoCD Image Updater가 자동 감지하여 롤아웃.

**Tech Stack:** Kubernetes, CNPG PostgreSQL (taxon ns), Vault + VSO, Traefik v3, ArgoCD + Image Updater, Woodpecker CI, goreleaser, Kustomize

---

## 파일 구조

```
morphso-hub/                          (private repo)
  cmd/server/main.go                  # MIGRATIONS_PATH env var 수정
  deploy/
    k8s/
      kustomization.yaml
      namespace.yaml
      vso-morphso.yaml                # Vault Secrets Operator
      configmap.yaml
      deployment.yaml
      service.yaml
      ingressroute.yaml
    argocd-apps/
      morphso-hub.yaml
  .woodpecker.yml

morphso/                              (open-source repo)
  .goreleaser.yaml
  .woodpecker.yml
```

---

## Task 1: 마이그레이션 경로 수정 (컨테이너 호환)

**Files:**
- Modify: `morphso-hub/cmd/server/main.go` (line 23)

**배경:** Dockerfile은 마이그레이션을 `/migrations`에 복사하지만 `main.go`는 상대경로 `"internal/db/migrations"`를 사용한다. 컨테이너에서는 바이너리가 `/`에서 실행되므로 `/internal/db/migrations`를 찾아 실패한다.

- [ ] **Step 1: main.go 수정**

`/Users/dong-hoshin/Documents/dev/morphso-hub/cmd/server/main.go` 수정:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/tojiuni/morphso-hub/internal/api"
	"github.com/tojiuni/morphso-hub/internal/auth"
	billingpkg "github.com/tojiuni/morphso-hub/internal/billing"
	"github.com/tojiuni/morphso-hub/internal/config"
	"github.com/tojiuni/morphso-hub/internal/db"
	"github.com/tojiuni/morphso-hub/internal/recommend"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "internal/db/migrations"
	}
	if err := db.RunMigrations(cfg.DatabaseURL, migrationsPath); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	store := registry.NewPGStore(pool)
	billingStore := billingpkg.NewPGStore(pool)
	authMW := auth.NewMiddleware(cfg.ZitadelIssuer)
	engine := recommend.NewEngine()
	stripeService := billingpkg.NewStripeClient(cfg.StripeSecretKey, cfg.StripeWebhookSecret)

	router := api.NewRouter(pool, store, authMW, engine, billingStore, stripeService)

	log.Printf("morphso-hub listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: 빌드 + 테스트 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go build ./cmd/server
go test ./... -race 2>&1 | tail -10
```
Expected: 빌드 성공, 기존 테스트 모두 PASS

---

## Task 2: DB 프로비저닝 + Vault 시크릿 초기화

**Prerequisites:** Vault port-forward 활성화 (`localhost:8200`), kubectl cluster access

- [ ] **Step 1: postgres superuser 비밀번호 조회**

```bash
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=$(cat ~/.vault-token)
vault kv get -field=password secret/neunexus/postgres
```
→ `<PG_SUPER_PASS>` 기록

- [ ] **Step 2: morphso 데이터베이스 + 유저 생성**

```bash
kubectl -n taxon run psql-morphso-init --rm -it --restart=Never \
  --image=postgres:16-alpine -- \
  psql "postgresql://postgres:<PG_SUPER_PASS>@postgres-rw.taxon.svc:5432/postgres"
```

psql 접속 후 실행 (강한 비밀번호 생성: `openssl rand -base64 24`):
```sql
CREATE USER morphso WITH PASSWORD '<MORPHSO_DB_PASS>';
CREATE DATABASE morphso OWNER morphso;
GRANT ALL PRIVILEGES ON DATABASE morphso TO morphso;
\q
```

- [ ] **Step 3: Redis + artifact-keeper 정보 수집**

```bash
# Redis 비밀번호
vault kv get -field=password secret/neunexus/redis
# → <REDIS_PASS>

# artifact-keeper 토큰: artifacts.toji.homes 관리자 UI에서 morphso-hub용 신규 발급
# 또는 기존 토큰 재사용
# → <AK_TOKEN>
```

- [ ] **Step 4: Vault에 morphso 시크릿 등록**

```bash
vault kv put secret/neunexus/morphso \
  database_url="postgres://morphso:<MORPHSO_DB_PASS>@postgres-rw.taxon.svc:5432/morphso" \
  redis_url="redis://:<REDIS_PASS>@redis-master.taxon.svc:6379" \
  stripe_secret_key="<STRIPE_SECRET_KEY>" \
  stripe_webhook_secret="<STRIPE_WEBHOOK_SECRET>" \
  artifact_keeper_token="<AK_TOKEN>"
```

- [ ] **Step 5: 시크릿 확인**

```bash
vault kv get secret/neunexus/morphso
```
Expected: `database_url`, `redis_url`, `stripe_secret_key`, `stripe_webhook_secret`, `artifact_keeper_token` 5개 키 출력

---

## Task 3: ZITADEL morphso-cli 앱 등록 + login.go 업데이트

- [ ] **Step 1: ZITADEL에서 morphso-cli Native 앱 등록**

1. `https://auth.toji.homes` 접속 → 관리자 로그인
2. Projects → (신규) `morphso` 프로젝트 생성 또는 기존 선택
3. Applications → **New Application**: Name=`morphso-cli`, Type=`Native`
4. Authentication Method: `None` (공개 클라이언트, Device Flow용)
5. Redirect URIs: (불필요, Device Flow는 redirect 없음)
6. Grant Types: `Device Code`, `Refresh Token` 활성화
7. **Client ID** 복사 → `<ZITADEL_CLI_CLIENT_ID>`

- [ ] **Step 2: cmd/login.go 클라이언트 ID 업데이트**

`/Users/dong-hoshin/Documents/dev/morphso/cmd/login.go` 수정:

```go
const zitadelIssuer = "https://auth.toji.homes"
const zitadelClientID = "<ZITADEL_CLI_CLIENT_ID>"  // ZITADEL에서 발급받은 실제 ID로 교체
```

`flow := auth.NewDeviceFlow(zitadelIssuer, "morphso-cli")` 를 다음으로 변경:
```go
flow := auth.NewDeviceFlow(zitadelIssuer, zitadelClientID)
```

- [ ] **Step 3: 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
```
Expected: 오류 없음

---

## Task 4: K8s 기본 리소스 (namespace, VSO, ConfigMap, Kustomize)

**Files:**
- Create: `morphso-hub/deploy/k8s/namespace.yaml`
- Create: `morphso-hub/deploy/k8s/vso-morphso.yaml`
- Create: `morphso-hub/deploy/k8s/configmap.yaml`
- Create: `morphso-hub/deploy/k8s/kustomization.yaml`

- [ ] **Step 1: 디렉터리 생성**

```bash
mkdir -p /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s
mkdir -p /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/argocd-apps
```

- [ ] **Step 2: namespace.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/namespace.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: morphso
```

- [ ] **Step 3: vso-morphso.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/vso-morphso.yaml`:

```yaml
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultStaticSecret
metadata:
  name: morphso-secrets
  namespace: morphso
spec:
  type: kv-v2
  mount: secret
  path: neunexus/morphso
  destination:
    name: morphso-secrets
    create: true
  refreshAfter: 1h
  vaultAuthRef: default
```

**Note:** `vaultAuthRef: default` 는 neunexus 클러스터의 기본 VaultAuth 리소스를 참조한다. 클러스터의 기존 VaultAuth 이름을 확인하려면: `kubectl get vaultauth -A`

- [ ] **Step 4: configmap.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: morphso-hub-config
  namespace: morphso
data:
  ZITADEL_ISSUER: "https://auth.toji.homes"
  ARTIFACT_KEEPER_URL: "https://artifacts.toji.homes"
  MIGRATIONS_PATH: "/migrations"
  PORT: "8080"
```

- [ ] **Step 5: kustomization.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - namespace.yaml
  - vso-morphso.yaml
  - configmap.yaml
  - deployment.yaml
  - service.yaml
  - ingressroute.yaml
```

- [ ] **Step 6: namespace + ConfigMap 적용 및 확인**

```bash
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/namespace.yaml
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/configmap.yaml
kubectl -n morphso get configmap morphso-hub-config
```
Expected: ConfigMap 생성 확인

- [ ] **Step 7: VSO 적용 + Secret 동기화 확인**

```bash
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/vso-morphso.yaml

# VSO가 Secret을 생성할 때까지 대기 (최대 60초)
kubectl -n morphso wait secret/morphso-secrets --for=jsonpath='{.metadata.name}'=morphso-secrets --timeout=60s

# Secret 키 목록 확인
kubectl -n morphso get secret morphso-secrets -o jsonpath='{.data}' | \
  python3 -c "import sys,json; [print(k) for k in json.load(sys.stdin)]"
```
Expected: `database_url`, `redis_url`, `stripe_secret_key`, `stripe_webhook_secret`, `artifact_keeper_token` 출력

---

## Task 5: Deployment + Service

**Files:**
- Create: `morphso-hub/deploy/k8s/deployment.yaml`
- Create: `morphso-hub/deploy/k8s/service.yaml`

- [ ] **Step 1: service.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: morphso-hub
  namespace: morphso
spec:
  selector:
    app: morphso-hub
  ports:
    - port: 8080
      targetPort: 8080
  type: ClusterIP
```

- [ ] **Step 2: deployment.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: morphso-hub
  namespace: morphso
  labels:
    app: morphso-hub
spec:
  replicas: 1
  selector:
    matchLabels:
      app: morphso-hub
  template:
    metadata:
      labels:
        app: morphso-hub
    spec:
      containers:
        - name: morphso-hub
          image: artifacts.toji.homes/morphso/morphso-hub:latest
          ports:
            - containerPort: 8080
          envFrom:
            - configMapRef:
                name: morphso-hub-config
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: morphso-secrets
                  key: database_url
            - name: REDIS_URL
              valueFrom:
                secretKeyRef:
                  name: morphso-secrets
                  key: redis_url
            - name: STRIPE_SECRET_KEY
              valueFrom:
                secretKeyRef:
                  name: morphso-secrets
                  key: stripe_secret_key
            - name: STRIPE_WEBHOOK_SECRET
              valueFrom:
                secretKeyRef:
                  name: morphso-secrets
                  key: stripe_webhook_secret
            - name: ARTIFACT_KEEPER_TOKEN
              valueFrom:
                secretKeyRef:
                  name: morphso-secrets
                  key: artifact_keeper_token
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 15
            periodSeconds: 20
          resources:
            requests:
              memory: "128Mi"
              cpu: "50m"
            limits:
              memory: "512Mi"
              cpu: "500m"
```

- [ ] **Step 3: 이미지 빌드 + 로컬 검증 (최초 배포 전)**

최초 배포 시 이미지가 아직 레지스트리에 없으므로 로컬에서 먼저 빌드 후 푸시한다:

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub

# 로컬 빌드
docker build -t artifacts.toji.homes/morphso/morphso-hub:latest .

# 레지스트리 로그인 (woodpecker-ci 토큰 사용)
vault kv get -field=artifacts-push-token secret/neunexus/woodpecker | \
  docker login artifacts.toji.homes -u woodpecker-ci --password-stdin

# 푸시
docker push artifacts.toji.homes/morphso/morphso-hub:latest
```

- [ ] **Step 4: Deployment + Service 적용 후 Pod 상태 확인**

```bash
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/service.yaml
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/deployment.yaml

# Pod Ready 대기 (최대 120초)
kubectl -n morphso rollout status deployment/morphso-hub --timeout=120s
```
Expected: `deployment "morphso-hub" successfully rolled out`

- [ ] **Step 5: /health 엔드포인트 확인 (클러스터 내부)**

```bash
kubectl -n morphso run curl-test --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -s http://morphso-hub.morphso.svc:8080/health
```
Expected: `{"status":"ok"}`

---

## Task 6: Traefik IngressRoute

**Files:**
- Create: `morphso-hub/deploy/k8s/ingressroute.yaml`

- [ ] **Step 1: Vault domains에 morphso 서브도메인 추가**

```bash
# 기존 domains secret 확인
vault kv get secret/neunexus/domains

# morphso 서브도메인 추가 (기존 키는 유지, 신규 키만 추가)
vault kv patch secret/neunexus/domains \
  subdomain_morphso="morphso"
```

- [ ] **Step 2: ingressroute.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/ingressroute.yaml`:

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: morphso-hub
  namespace: morphso
spec:
  entryPoints:
    - websecure
  routes:
    - match: "Host(`morphso.toji.homes`)"
      kind: Rule
      services:
        - name: morphso-hub
          port: 8080
  tls:
    certResolver: cloudflare
```

- [ ] **Step 3: IngressRoute 적용 + HTTPS 확인**

```bash
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/k8s/ingressroute.yaml

# 외부에서 health 확인
curl -s https://morphso.toji.homes/health
```
Expected: `{"status":"ok"}`

---

## Task 7: ArgoCD Application

**Files:**
- Create: `morphso-hub/deploy/argocd-apps/morphso-hub.yaml`

**배경:** ArgoCD Image Updater가 `artifacts.toji.homes/morphso/morphso-hub` 이미지의 새 digest를 감지하면 자동으로 Deployment를 업데이트한다. Woodpecker CI가 이미지를 푸시하면 별도 deploy step 없이 자동 롤아웃된다.

- [ ] **Step 1: morphso-hub.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/deploy/argocd-apps/morphso-hub.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: morphso-hub
  namespace: metaflow
  annotations:
    argocd-image-updater.argoproj.io/image-list: "app=artifacts.toji.homes/morphso/morphso-hub"
    argocd-image-updater.argoproj.io/app.update-strategy: digest
    argocd-image-updater.argoproj.io/write-back-method: argocd
spec:
  project: default
  source:
    repoURL: https://git.toji.homes/tojiuni/morphso-hub.git
    targetRevision: HEAD
    path: deploy/k8s
    kustomize:
      images:
        - artifacts.toji.homes/morphso/morphso-hub:latest
  destination:
    server: https://kubernetes.default.svc
    namespace: morphso
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

- [ ] **Step 2: ArgoCD에 Application 등록**

```bash
# ArgoCD가 metaflow ns에서 실행 중이므로 해당 ns에 적용
kubectl apply -f /Users/dong-hoshin/Documents/dev/morphso-hub/deploy/argocd-apps/morphso-hub.yaml
```

- [ ] **Step 3: ArgoCD Sync 확인**

```bash
# ArgoCD pod에서 CLI로 확인하거나 UI에서 확인
kubectl -n metaflow exec deploy/argocd-server -- \
  argocd app get morphso-hub --server localhost:8080 --plaintext

# 또는 Application 상태 조회
kubectl -n metaflow get application morphso-hub -o jsonpath='{.status.sync.status}'
```
Expected: `Synced`

- [ ] **Step 4: ArgoCD가 morphso-hub ns 접근권한 있는지 확인**

ArgoCD가 새 네임스페이스 `morphso`에 배포할 권한이 없으면 RBAC 추가 필요:
```bash
kubectl -n metaflow get application morphso-hub -o jsonpath='{.status.conditions}'
```
오류가 있으면: ArgoCD AppProject에서 `morphso` ns를 허용 대상에 추가.

---

## Task 8: morphso-hub Woodpecker CI 파이프라인

**Files:**
- Create: `morphso-hub/.woodpecker.yml`

**배경:** `labels: platform: linux/amd64` 는 neunexus 필수 조건 (2026-05-11 darwin 에이전트 추가 이후). Woodpecker 시크릿 `artifacts_push_token` 은 `secret/neunexus/woodpecker` → `artifacts-push-token` 에서 수동 등록 필요.

- [ ] **Step 1: .woodpecker.yml 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/.woodpecker.yml`:

```yaml
labels:
  platform: linux/amd64

steps:
  - name: test
    image: golang:1.23-alpine
    commands:
      - go test ./... -race
    when:
      event: [push, pull_request]

  - name: build-push
    image: plugins/docker
    settings:
      registry: artifacts.toji.homes
      repo: artifacts.toji.homes/morphso/morphso-hub
      username: woodpecker-ci
      password:
        from_secret: artifacts_push_token
      tags:
        - latest
        - ${CI_COMMIT_SHA:0:8}
    when:
      event: push
      branch: main
```

- [ ] **Step 2: Woodpecker에 morphso-hub 레포 등록**

1. `https://ci.toji.homes` → 로그인
2. Repositories → `tojiuni/morphso-hub` 활성화
3. Settings → Secrets → `artifacts_push_token` 추가:
   - 값: `vault kv get -field=artifacts-push-token secret/neunexus/woodpecker`

- [ ] **Step 3: 파이프라인 실행 확인**

morphso-hub `main` 브랜치에 커밋 push 후:
```bash
# Woodpecker UI에서 파이프라인 확인: https://ci.toji.homes
# 또는 이미지 레지스트리에서 확인
curl -s -u "woodpecker-ci:<AK_TOKEN>" \
  https://artifacts.toji.homes/v2/morphso/morphso-hub/tags/list
```
Expected: `latest` + SHA 태그 확인

- [ ] **Step 4: ArgoCD 자동 롤아웃 확인**

이미지 푸시 후 수 분 내:
```bash
kubectl -n morphso rollout status deployment/morphso-hub
```
Expected: 새 이미지로 롤아웃 완료

---

## Task 9: morphso CLI goreleaser + Woodpecker CI

**Files:**
- Create: `morphso/.goreleaser.yaml`
- Create: `morphso/.woodpecker.yml`

- [ ] **Step 1: .goreleaser.yaml 작성**

`/Users/dong-hoshin/Documents/dev/morphso/.goreleaser.yaml`:

```yaml
version: 2

project_name: morphso

before:
  hooks:
    - go mod tidy

builds:
  - main: .
    binary: morphso
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: checksums.txt

release:
  github:
    owner: tojiuni
    name: morphso
  draft: false

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
```

- [ ] **Step 2: goreleaser 로컬 검증 (snapshot 모드)**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser release --snapshot --clean
```
Expected: `dist/` 아래 멀티플랫폼 바이너리 생성 (darwin_arm64, darwin_amd64, linux_amd64, linux_arm64, windows_amd64)

- [ ] **Step 3: .woodpecker.yml 작성**

`/Users/dong-hoshin/Documents/dev/morphso/.woodpecker.yml`:

```yaml
labels:
  platform: linux/amd64

steps:
  - name: test
    image: golang:1.23-alpine
    commands:
      - go test ./... -race
    when:
      event: [push, pull_request, tag]

  - name: release
    image: goreleaser/goreleaser:v2
    commands:
      - goreleaser release --clean
    environment:
      GITHUB_TOKEN:
        from_secret: github_token
    when:
      event: tag
```

- [ ] **Step 4: Woodpecker에 morphso 레포 등록 + 시크릿 추가**

1. `https://ci.toji.homes` → `tojiuni/morphso` 활성화 (또는 `lyckabc/morphso`)
2. Settings → Secrets → `github_token` 추가:
   - 값: GitHub Personal Access Token (repo scope + write:packages)
   - `vault kv get -field=token secret/neunexus/tojismith/github_token` (기존 토큰 재사용 가능)

- [ ] **Step 5: 첫 릴리스 태그 + 파이프라인 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git tag v0.1.0
git push origin v0.1.0
```

Woodpecker UI → release 스텝 완료 후:
```bash
# GitHub Releases에서 바이너리 확인
gh release view v0.1.0 --repo tojiuni/morphso
```
Expected: darwin/linux/windows 바이너리 + checksums.txt 업로드 확인

---

## 스펙 자체 검토

**스펙 커버리지:**
- ✅ morphso-hub K8s Deployment (morphso ns) → Task 5
- ✅ ClusterIP Service → Task 5
- ✅ Traefik IngressRoute (morphso.toji.homes) → Task 6
- ✅ CNPG morphso-db (taxon ns 클러스터 활용) → Task 2
- ✅ Redis (taxon ns 기존 재사용, redis_url via Vault) → Task 2
- ✅ Vault 시크릿 경로 설정 (`secret/neunexus/morphso/`) → Task 2
- ✅ VSO Vault→K8s Secret 동기화 → Task 4
- ✅ ZITADEL morphso-cli Native 앱 등록 → Task 3
- ✅ Woodpecker CI morphso-hub (test + build + push) → Task 8
- ✅ ArgoCD Application + Image Updater 자동 배포 → Task 7
- ✅ Woodpecker CI morphso CLI (test + goreleaser release) → Task 9
- ✅ goreleaser 멀티플랫폼 (darwin/linux/windows, amd64/arm64) → Task 9
- ✅ MIGRATIONS_PATH 컨테이너 호환 수정 → Task 1
