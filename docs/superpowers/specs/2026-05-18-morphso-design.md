# MorphSo 설계 문서

**날짜**: 2026-05-18  
**상태**: 승인됨

---

## 개요

MorphSo([IPA: mɔːrf.soʊ], Morphogen + 所)는 AI 전용 서비스 마켓플레이스다. CLI를 통해 AI 관련 서비스·라이브러리·패키지를 쉽게 설치·관리할 수 있으며, 사용자의 OS 스펙을 분석해 최적의 설치 방식을 AI가 추천한다. 개발자와 비개발자 모두를 대상으로 한다.

### 이름 어원

- **Morphe** (μορφή): 형태, 모양
- **So** (所): 장소 (한자)
- → "모양을 만드는 장소" — AI 서비스가 형태를 갖추는 공간

---

## 레포지토리 구성

| 레포 | 설명 | 공개 여부 |
|------|------|---------|
| `morphso` | CLI 클라이언트 (Go) | 오픈소스 |
| `morphso-hub` | SaaS 백엔드 서버 (Go) | 비공개 |

---

## 아키텍처

### 전체 구성도

```
[morphso CLI (Go, 단일 바이너리)]
   사용자 머신에서 실행
         │
         │ HTTPS REST API
         ▼
[morphso-hub (Go, morphso ns @ neunexus K8s)]
   ├─ /registry   ─► Package 메타데이터 (Postgres · taxon ns)
   ├─ /auth       ─► ZITADEL OIDC (lymphhub ns)
   ├─ /billing    ─► Stripe / Toss Payments
   ├─ /spec       ─► OS Spec DB (Postgres)
   ├─ /recommend  ─► AI 추천 엔진 (rule-based + LLM)
   └─ /files      ─► artifact-keeper (metaviewer ns)
         │
         └─ Cache ─► Redis (taxon ns)

[Woodpecker CI] ─► morphso-hub 이미지 빌드/배포
[Traefik]       ─► morphso.toji.homes
[Vault]         ─► morphso-hub 시크릿 주입
```

### neunexus 레이어 의존 관계

```
Osteon (Vault · Traefik · Cinder)
  └─ taxon ─── Postgres(CNPG) · Redis · Qdrant
       ├─ lymphhub ─── ZITADEL · SpiceDB
       │    ├─ metaviewer ─ Forgejo · Vikunja · artifact-keeper
       │    └─ morphso ──── morphso-hub  ← 신규
       └─ proprio / metaflow / ai-assistant / ...
```

---

## CLI 설계 (morphso)

### 커맨드 구조

```bash
# 설치
morphso install <package>                    # AI 추천 strategy 자동 선택
morphso install <package> --native           # pip/npm/binary 로컬 설치
morphso install <package> --docker           # Docker 컨테이너
morphso install <package> --k8s             # K8s 클러스터 배포
morphso install <package> --helm            # Helm chart 배포
morphso install <package> --yes             # 비대화형 (bot 호출용)
morphso install <package> --yes --strategy=docker  # strategy 강제 지정

# 패키지 관리
morphso search <query>                       # 패키지 검색
morphso info <package>                       # 상세 정보 + 지원 strategy 목록
morphso list                                 # 설치된 패키지 목록
morphso update <package>                     # 업데이트
morphso remove <package>                     # 제거

# 계정
morphso login                                # ZITADEL Device Flow 인증
morphso logout
morphso whoami                               # 현재 로그인 상태

# 배포
morphso publish                              # 패키지 배포
```

### 로컬 파일 구조

```
~/.morphso/
  ├─ config.yaml     # auth token, hub URL
  └─ spec.yaml       # 캐시된 OS 스펙 (TTL: 24h)
```

```yaml
# spec.yaml 예시
collected_at: 2026-05-18T10:00:00Z
os: darwin
arch: arm64
os_version: "15.2"
memory_total_gb: 32
memory_free_gb: 18
disk_total_gb: 500
disk_free_gb: 210
gpu: null
installed_tools:
  docker: "27.1.0"
  kubectl: "1.35.0"
  helm: "3.15.0"
  pip: "24.0"
  npm: null
  brew: "4.3.0"
```

### 부트스트랩 설치

```bash
# curl 스크립트 (런타임 불필요)
curl -fsSL https://morphso.io/install | sh

# Homebrew
brew install morphso
```

---

## AI 기반 설치 플로우

```
morphso install gopedia
  │
  ├─ 1. ~/.morphso/spec.yaml 확인
  │       없거나 24h 경과 → OS 스펙 재수집 후 저장
  │       수집 항목: os, arch, os_version, memory, disk, gpu,
  │                 installed_tools (docker/kubectl/helm/pip/npm/brew 버전)
  │
  ├─ 2. morphso-hub /recommend 요청
  │       { package: "gopedia", spec: {...}, preferred_strategy: null }
  │
  ├─ 3. hub AI 엔진 분석
  │       1단계: rule-based 스코어링 (대부분 케이스)
  │         - kubectl 있음 + 메모리 >4GB  → k8s
  │         - docker 있음 + 메모리 >2GB   → docker
  │         - 그 외                       → native
  │       2단계: LLM (Ollama / Claude API) — 조건 충돌 또는 복잡한 케이스
  │
  ├─ 4. 추천 결과 출력 (human)
  │       > Recommended: --k8s
  │       > Reason: kubectl detected, 18GB RAM free, 관리 일관성에 유리
  │       > Proceed? [Y/n/--docker/--native]
  │       또는 --yes 플래그 → 자동 수락 (bot)
  │
  ├─ 5. hub에서 install plan 수신
  │       {
  │         prerequisites: ["kubectl", "helm"],  // 없으면 자동 설치
  │         steps: [...],
  │         post_install: [...]
  │       }
  │
  └─ 6. CLI 실행
         - prerequisite 없으면 자동 설치 (pip, npm, brew, kubectl 등)
         - steps 순서대로 실행
         - 로그인 상태면 설치 이력 → hub에 기록
```

---

## 패키지 모델 & 레지스트리

### 패키지 데이터 모델 (Postgres)

```sql
packages
  id, name, slug, description, author_id
  type: pip | npm | binary | mcp | recipe | helm
  visibility: public | private
  verified: bool                    -- morphso 팀 검수 배지
  price_cents: int                  -- 0 = 무료
  recommended_strategy: native | docker | k8s | helm
  downloads: int
  created_at, updated_at

package_versions
  id, package_id, version           -- semver
  changelog, published_at
  artifact_url                      -- artifact-keeper 파일 경로

package_strategies
  id, package_version_id
  strategy: native | docker | k8s | helm
  os_variants: jsonb                -- [{os, arch, steps[], prerequisites[]}]
  resource_requirements: jsonb      -- {min_memory_gb, min_disk_gb, needs_gpu}

package_tags
  package_id, tag                   -- mcp, langchain, llm, rag, etc.

install_history                     -- 로그인 유저만 기록
  id, user_id, package_id, version, strategy, os, arch, installed_at
```

### 패키지 스펙 예시

```yaml
name: gopedia
type: mcp
recommended_strategy: k8s
strategies:
  native:
    os_variants:
      - os: darwin
        arch: arm64
        prerequisites: [pip]
        steps:
          - pip install gopedia==1.2.0
          - morphso-post-install gopedia
      - os: linux
        arch: amd64
        prerequisites: [pip]
        steps:
          - pip install gopedia==1.2.0
  docker:
    image: artifacts.toji.homes/gopedia:1.2.0
    run: docker run -d -p 8080:8080 {image}
    resource_requirements:
      min_memory_gb: 2
  k8s:
    source: artifact-keeper
    chart: gopedia
    resource_requirements:
      min_memory_gb: 4
  helm:
    repo: https://artifacts.toji.homes/helm
    chart: gopedia
```

### 레지스트리 소스 계층

```
morphso install <package> 요청 시 hub 탐색 순서:

1. morphso 자체 레지스트리
   → Postgres 메타 + artifact-keeper 파일
   → morphso verified 패키지, 전용 AI 패키지

2. artifact-keeper 직접 연동
   → artifacts.toji.homes 기존 패키지 (goquest, gopedia 등)

3. 외부 레지스트리 오케스트레이션 (recipe 방식)
   → pip(PyPI), npm(npmjs.com), brew(Homebrew)
   → morphso-hub가 recipe 정의, CLI가 실행
```

---

## Auth

### 플로우

```
morphso login
  → ZITADEL Device Flow
  → 터미널: "https://auth.toji.homes/activate 에서 코드 XXXX-XXXX 입력"
  → 브라우저 인증 완료
  → OIDC access token → ~/.morphso/config.yaml 저장

morphso-hub
  → 모든 API 요청: Authorization: Bearer <token>
  → ZITADEL JWKS 엔드포인트로 서명 검증
  → user_id, email, subscription_tier 클레임 추출
```

### ZITADEL 앱 등록

| 앱 | 타입 | 용도 |
|----|------|------|
| morphso-hub | API | token 검증 |
| morphso-cli | Native (Device Flow) | CLI 로그인 |

---

## Billing

### 결제 데이터 모델 (Postgres)

```sql
subscriptions
  user_id, tier: free | pro
  stripe_subscription_id
  valid_until

package_purchases
  id, buyer_id, package_id, version
  amount_cents, purchased_at
  stripe_payment_intent_id

publisher_earnings
  id, publisher_id, package_purchase_id
  gross_cents                         -- 구매자가 낸 금액
  fee_cents                           -- gross × 1% (morphso 수수료)
  net_cents                           -- gross - fee (퍼블리셔 수령액)
  status: pending | paid_out
  paid_out_at

payouts
  id, publisher_id
  amount_cents, stripe_transfer_id
  created_at
```

### 결제 흐름

```
[유료 패키지 구매]
사용자 결제 (Stripe)
  → morphso-hub: payment_intent 검증
  → package_purchases 기록
  → publisher_earnings 생성
      gross: 10,000원
      fee:     100원  (1% morphso 수수료)
      net:   9,900원  ← 퍼블리셔 수령
  → 패키지 설치 권한 부여

[퍼블리셔 정산]
  → Stripe Connect Transfer
  → 퍼블리셔 Stripe 계좌로 net_cents 송금
  → payout 기록
```

### 티어별 기능

| 항목 | 비로그인 | 무료 | Pro (유료 구독) |
|------|---------|------|----------------|
| 설치 | 공개 무료만 | 공개 무료 전체 | 전체 + 구매한 유료 |
| 배포 | 불가 | 공개 3개 (무료만) | 무제한 + private + 유료 패키지 |
| 파일 크기 | — | 100MB/패키지 | 2GB/패키지 |
| 수익 정산 | — | — | Stripe Connect (수수료 1%) |
| 설치 이력 | — | 저장 | 저장 |

---

## neunexus 배포 구성

### K8s 리소스 (morphso ns)

```yaml
Deployment:   morphso-hub (Go binary, 단일 컨테이너)
Service:      ClusterIP
IngressRoute: morphso.toji.homes (Traefik + cert-manager TLS)
Database:     morphso-db (CNPG, taxon ns 클러스터 활용)
Redis:        taxon ns 기존 Redis 재사용
```

### Vault 시크릿 경로

```
secret/neunexus/morphso/
  ├─ db_password
  ├─ zitadel_client_id
  ├─ zitadel_client_secret
  ├─ stripe_secret_key
  ├─ stripe_webhook_secret
  └─ artifact_keeper_token
```

### artifact-keeper 연동

```
morphso-hub → artifact-keeper REST API
파일 경로: artifacts.toji.homes/morphso/<pkg>/<version>/<file>
```

### CI/CD

```
morphso-hub (Forgejo)
  push → Woodpecker CI
    ├─ go test ./...
    ├─ go build → Docker image
    ├─ push → artifact-keeper (container registry)
    └─ ArgoCD sync → morphso ns 배포

morphso (Forgejo + GitHub 미러)
  push → Woodpecker CI
    ├─ go test ./...
    ├─ goreleaser → 멀티플랫폼 바이너리
    │   darwin/arm64, darwin/amd64
    │   linux/amd64, linux/arm64
    │   windows/amd64
    └─ GitHub Releases + morphso-hub 바이너리 등록
```

---

## 구현 범위 (MVP vs 후속)

| 기능 | MVP | 후속 |
|------|:---:|:----:|
| CLI 기본 커맨드 (install/search/list/remove) | ✅ | |
| OS 스펙 수집 & 캐시 | ✅ | |
| rule-based 설치 추천 | ✅ | |
| ZITADEL 로그인 | ✅ | |
| 무료 패키지 레지스트리 | ✅ | |
| artifact-keeper 연동 | ✅ | |
| install strategy (native/docker/k8s/helm) | ✅ | |
| 설치 이력 저장 | ✅ | |
| 결제 (Stripe 구독 + 마켓플레이스) | ✅ | |
| 퍼블리셔 정산 (Stripe Connect) | ✅ | |
| LLM 기반 고급 추천 | | ✅ |
| 패키지 추천 엔진 (개인화) | | ✅ |
| pip/npm 호환 index server | | ✅ |
| 웹 UI (마켓플레이스 브라우저) | | ✅ |
