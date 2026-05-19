# morphso-hub Billing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** morphso-hub에 Stripe 기반 결제 시스템 구축 — Pro 구독, 유료 패키지 구매, 퍼블리셔 정산(수수료 1%).

**Architecture:** StripeService 인터페이스로 Stripe SDK를 추상화해 핸들러를 mock 테스트 가능하게 만든다. Stripe 결제는 PaymentIntent 방식(클라이언트에서 Stripe.js로 확인), 구독은 Subscription API, 정산은 Stripe Connect Transfer. Webhook(`payment_intent.succeeded`)에서 구매 기록 + 퍼블리셔 수익 계산.

**Tech Stack:** stripe-go/v82 (이미 설치됨), pgx/v5, chi v5, Go 1.23

**기존 코드 위치**: `/Users/dong-hoshin/Documents/dev/morphso-hub`

---

## 파일 구조

```
internal/
  billing/
    domain.go          # Subscription, PackagePurchase, PublisherEarnings, Payout 타입
    service.go         # StripeService 인터페이스 + 실제 구현체
    service_test.go    # mock 서버 기반 StripeService 테스트
    store.go           # BillingStore 인터페이스 + pgStore 구현
    store_test.go      # mock store 기반 테스트
  db/migrations/
    003_billing.sql    # subscriptions, package_purchases, publisher_earnings, payouts
  api/handlers/
    billing.go         # HTTP 핸들러 (구독, 구매, 수익, 정산, webhook)
    billing_test.go    # 핸들러 단위 테스트
  api/
    router.go          # MODIFY: billing 라우트 추가
  config/
    config.go          # MODIFY: Stripe 환경변수 추가
```

---

## Task 1: Billing DB 마이그레이션

**Files:**
- Create: `internal/db/migrations/003_billing.sql`

- [ ] **Step 1: 마이그레이션 파일 작성**

```sql
-- internal/db/migrations/003_billing.sql
-- +migrate Up
CREATE TABLE subscriptions (
    user_id                TEXT PRIMARY KEY,
    tier                   TEXT NOT NULL DEFAULT 'free',
    stripe_subscription_id TEXT,
    valid_until            TIMESTAMPTZ
);

CREATE TABLE package_purchases (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_id                 TEXT NOT NULL,
    package_slug             TEXT NOT NULL,
    version                  TEXT NOT NULL,
    amount_cents             INTEGER NOT NULL,
    purchased_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stripe_payment_intent_id TEXT NOT NULL UNIQUE
);

CREATE INDEX idx_purchases_buyer ON package_purchases(buyer_id);
CREATE INDEX idx_purchases_slug  ON package_purchases(package_slug);

CREATE TABLE publisher_earnings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    publisher_id        TEXT NOT NULL,
    package_purchase_id UUID NOT NULL REFERENCES package_purchases(id),
    gross_cents         INTEGER NOT NULL,
    fee_cents           INTEGER NOT NULL,
    net_cents           INTEGER NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    paid_out_at         TIMESTAMPTZ
);

CREATE INDEX idx_earnings_publisher ON publisher_earnings(publisher_id);
CREATE INDEX idx_earnings_status    ON publisher_earnings(publisher_id, status);

CREATE TABLE payouts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    publisher_id       TEXT NOT NULL,
    amount_cents       INTEGER NOT NULL,
    stripe_transfer_id TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payouts_publisher ON payouts(publisher_id);

-- +migrate Down
DROP TABLE IF EXISTS payouts;
DROP TABLE IF EXISTS publisher_earnings;
DROP TABLE IF EXISTS package_purchases;
DROP TABLE IF EXISTS subscriptions;
```

- [ ] **Step 2: 컴파일 확인 (파일만 추가, Go 변경 없음)**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
ls internal/db/migrations/
```
Expected: `001_packages.sql  002_install_history.sql  003_billing.sql`

- [ ] **Step 3: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/db/migrations/003_billing.sql
git commit -m "feat: add billing tables migration"
git push origin main
```

---

## Task 2: Billing 도메인 타입

**Files:**
- Create: `internal/billing/domain.go`

- [ ] **Step 1: domain.go 작성**

```go
// internal/billing/domain.go
package billing

import "time"

type EarningsStatus string

const (
	StatusPending EarningsStatus = "pending"
	StatusPaidOut EarningsStatus = "paid_out"
)

type Subscription struct {
	UserID               string     `json:"user_id"`
	Tier                 string     `json:"tier"`
	StripeSubscriptionID string     `json:"stripe_subscription_id"`
	ValidUntil           *time.Time `json:"valid_until"`
}

type PackagePurchase struct {
	ID                    string    `json:"id"`
	BuyerID               string    `json:"buyer_id"`
	PackageSlug           string    `json:"package_slug"`
	Version               string    `json:"version"`
	AmountCents           int64     `json:"amount_cents"`
	PurchasedAt           time.Time `json:"purchased_at"`
	StripePaymentIntentID string    `json:"stripe_payment_intent_id"`
}

type PublisherEarnings struct {
	ID                string         `json:"id"`
	PublisherID       string         `json:"publisher_id"`
	PackagePurchaseID string         `json:"package_purchase_id"`
	GrossCents        int64          `json:"gross_cents"`
	FeeCents          int64          `json:"fee_cents"`
	NetCents          int64          `json:"net_cents"`
	Status            EarningsStatus `json:"status"`
	PaidOutAt         *time.Time     `json:"paid_out_at"`
}

type Payout struct {
	ID               string    `json:"id"`
	PublisherID      string    `json:"publisher_id"`
	AmountCents      int64     `json:"amount_cents"`
	StripeTransferID string    `json:"stripe_transfer_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// CalculateEarnings computes fee (1%) and net from gross amount.
func CalculateEarnings(grossCents int64) (feeCents, netCents int64) {
	feeCents = grossCents / 100 // 1%
	netCents = grossCents - feeCents
	return
}

type CreatePaymentIntentResponse struct {
	ClientSecret string `json:"client_secret"`
	AmountCents  int64  `json:"amount_cents"`
}

type SubscribeRequest struct {
	StripeConnectAccountID string `json:"stripe_connect_account_id,omitempty"`
}
```

- [ ] **Step 2: 컴파일 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go build ./internal/billing/...
```
Expected: 오류 없음

- [ ] **Step 3: CalculateEarnings 단위 테스트**

`internal/billing/domain_test.go`:

```go
package billing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso-hub/internal/billing"
)

func TestCalculateEarnings(t *testing.T) {
	tests := []struct {
		gross    int64
		wantFee  int64
		wantNet  int64
	}{
		{10000, 100, 9900},
		{100, 1, 99},
		{0, 0, 0},
		{1, 0, 1}, // 1% of 1 = 0 (integer division)
	}
	for _, tc := range tests {
		fee, net := billing.CalculateEarnings(tc.gross)
		assert.Equal(t, tc.wantFee, fee, "gross=%d fee", tc.gross)
		assert.Equal(t, tc.wantNet, net, "gross=%d net", tc.gross)
	}
}
```

- [ ] **Step 4: 테스트 실행**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/billing/... -v -race
```
Expected: `TestCalculateEarnings PASS`

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/billing/
git commit -m "feat: add billing domain types and fee calculation"
git push origin main
```

---

## Task 3: Billing Store (인터페이스 + pgStore)

**Files:**
- Create: `internal/billing/store.go`
- Create: `internal/billing/store_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/billing/store_test.go`:

```go
package billing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/billing"
)

type mockBillingStore struct {
	subscriptions map[string]*billing.Subscription
	purchases     []*billing.PackagePurchase
	earnings      []*billing.PublisherEarnings
	payouts       []*billing.Payout
	pkgPrices     map[string]int64
}

func newMockBillingStore() *mockBillingStore {
	return &mockBillingStore{
		subscriptions: make(map[string]*billing.Subscription),
		pkgPrices:     make(map[string]int64),
	}
}

func (m *mockBillingStore) GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error) {
	s, ok := m.subscriptions[userID]
	if !ok {
		return nil, billing.ErrNotFound
	}
	return s, nil
}

func (m *mockBillingStore) UpsertSubscription(ctx context.Context, sub *billing.Subscription) error {
	m.subscriptions[sub.UserID] = sub
	return nil
}

func (m *mockBillingStore) GetPackagePriceCents(ctx context.Context, slug string) (int64, error) {
	p, ok := m.pkgPrices[slug]
	if !ok {
		return 0, billing.ErrNotFound
	}
	return p, nil
}

func (m *mockBillingStore) RecordPurchase(ctx context.Context, purchase *billing.PackagePurchase) error {
	m.purchases = append(m.purchases, purchase)
	return nil
}

func (m *mockBillingStore) HasPurchased(ctx context.Context, buyerID, slug string) (bool, error) {
	for _, p := range m.purchases {
		if p.BuyerID == buyerID && p.PackageSlug == slug {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockBillingStore) RecordEarnings(ctx context.Context, e *billing.PublisherEarnings) error {
	m.earnings = append(m.earnings, e)
	return nil
}

func (m *mockBillingStore) GetPendingEarnings(ctx context.Context, publisherID string) (int64, error) {
	var total int64
	for _, e := range m.earnings {
		if e.PublisherID == publisherID && e.Status == billing.StatusPending {
			total += e.NetCents
		}
	}
	return total, nil
}

func (m *mockBillingStore) RecordPayout(ctx context.Context, payout *billing.Payout) error {
	m.payouts = append(m.payouts, payout)
	return nil
}

func (m *mockBillingStore) MarkEarningsPaidOut(ctx context.Context, publisherID, payoutID string) error {
	for _, e := range m.earnings {
		if e.PublisherID == publisherID && e.Status == billing.StatusPending {
			e.Status = billing.StatusPaidOut
		}
	}
	return nil
}

func TestBillingStore_SubscriptionRoundTrip(t *testing.T) {
	store := newMockBillingStore()
	sub := &billing.Subscription{UserID: "user-1", Tier: "pro", StripeSubscriptionID: "sub_abc"}
	require.NoError(t, store.UpsertSubscription(context.Background(), sub))

	got, err := store.GetSubscription(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Equal(t, "pro", got.Tier)
}

func TestBillingStore_GetSubscription_NotFound(t *testing.T) {
	store := newMockBillingStore()
	_, err := store.GetSubscription(context.Background(), "nobody")
	assert.ErrorIs(t, err, billing.ErrNotFound)
}

func TestBillingStore_HasPurchased(t *testing.T) {
	store := newMockBillingStore()
	require.NoError(t, store.RecordPurchase(context.Background(), &billing.PackagePurchase{
		BuyerID: "user-1", PackageSlug: "gopedia", Version: "1.0.0",
		AmountCents: 10000, StripePaymentIntentID: "pi_abc",
	}))

	ok, err := store.HasPurchased(context.Background(), "user-1", "gopedia")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestBillingStore_PendingEarnings(t *testing.T) {
	store := newMockBillingStore()
	require.NoError(t, store.RecordEarnings(context.Background(), &billing.PublisherEarnings{
		PublisherID: "pub-1", GrossCents: 10000, FeeCents: 100, NetCents: 9900,
		Status: billing.StatusPending,
	}))

	total, err := store.GetPendingEarnings(context.Background(), "pub-1")
	require.NoError(t, err)
	assert.Equal(t, int64(9900), total)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/billing/... -v
```
Expected: `FAIL` (ErrNotFound, Store 인터페이스 없음)

- [ ] **Step 3: store.go 구현**

`internal/billing/store.go`:

```go
package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	GetSubscription(ctx context.Context, userID string) (*Subscription, error)
	UpsertSubscription(ctx context.Context, sub *Subscription) error
	GetPackagePriceCents(ctx context.Context, slug string) (int64, error)
	RecordPurchase(ctx context.Context, purchase *PackagePurchase) error
	HasPurchased(ctx context.Context, buyerID, slug string) (bool, error)
	RecordEarnings(ctx context.Context, e *PublisherEarnings) error
	GetPendingEarnings(ctx context.Context, publisherID string) (int64, error)
	RecordPayout(ctx context.Context, payout *Payout) error
	MarkEarningsPaidOut(ctx context.Context, publisherID, payoutID string) error
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT user_id, tier, stripe_subscription_id, valid_until FROM subscriptions WHERE user_id = $1`,
		userID)
	var sub Subscription
	err := row.Scan(&sub.UserID, &sub.Tier, &sub.StripeSubscriptionID, &sub.ValidUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan subscription: %w", err)
	}
	return &sub, nil
}

func (s *pgStore) UpsertSubscription(ctx context.Context, sub *Subscription) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO subscriptions (user_id, tier, stripe_subscription_id, valid_until)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET tier = $2, stripe_subscription_id = $3, valid_until = $4`,
		sub.UserID, sub.Tier, sub.StripeSubscriptionID, sub.ValidUntil)
	return err
}

func (s *pgStore) GetPackagePriceCents(ctx context.Context, slug string) (int64, error) {
	row := s.pool.QueryRow(ctx, `SELECT price_cents FROM packages WHERE slug = $1`, slug)
	var price int64
	err := row.Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("scan price: %w", err)
	}
	return price, nil
}

func (s *pgStore) RecordPurchase(ctx context.Context, p *PackagePurchase) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO package_purchases (buyer_id, package_slug, version, amount_cents, stripe_payment_intent_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (stripe_payment_intent_id) DO NOTHING`,
		p.BuyerID, p.PackageSlug, p.Version, p.AmountCents, p.StripePaymentIntentID)
	return err
}

func (s *pgStore) HasPurchased(ctx context.Context, buyerID, slug string) (bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT COUNT(1) FROM package_purchases WHERE buyer_id = $1 AND package_slug = $2`,
		buyerID, slug)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("has purchased: %w", err)
	}
	return count > 0, nil
}

func (s *pgStore) RecordEarnings(ctx context.Context, e *PublisherEarnings) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO publisher_earnings (publisher_id, package_purchase_id, gross_cents, fee_cents, net_cents, status)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.PublisherID, e.PackagePurchaseID, e.GrossCents, e.FeeCents, e.NetCents, e.Status)
	return err
}

func (s *pgStore) GetPendingEarnings(ctx context.Context, publisherID string) (int64, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(net_cents), 0) FROM publisher_earnings WHERE publisher_id = $1 AND status = 'pending'`,
		publisherID)
	var total int64
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("sum earnings: %w", err)
	}
	return total, nil
}

func (s *pgStore) RecordPayout(ctx context.Context, p *Payout) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO payouts (publisher_id, amount_cents, stripe_transfer_id)
		VALUES ($1, $2, $3)`,
		p.PublisherID, p.AmountCents, p.StripeTransferID)
	return err
}

func (s *pgStore) MarkEarningsPaidOut(ctx context.Context, publisherID, payoutID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE publisher_earnings
		SET status = 'paid_out', paid_out_at = NOW()
		WHERE publisher_id = $1 AND status = 'pending'`,
		publisherID)
	return err
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/billing/... -v -race
```
Expected: 5개 테스트 PASS (CalculateEarnings 4, Store 4)

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/billing/store.go internal/billing/store_test.go
git commit -m "feat: add billing store interface and pg implementation"
git push origin main
```

---

## Task 4: Stripe 서비스 (인터페이스 + 구현)

**Files:**
- Create: `internal/billing/service.go`
- Create: `internal/billing/service_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/billing/service_test.go`:

```go
package billing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/billing"
)

// mockStripeService는 테스트용 StripeService 구현체입니다.
type mockStripeService struct {
	customerID   string
	intentSecret string
	intentID     string
	transferID   string
}

func (m *mockStripeService) CreateOrGetCustomer(email string) (string, error) {
	return m.customerID, nil
}

func (m *mockStripeService) CreatePaymentIntent(amountCents int64, customerID, packageSlug string) (string, string, error) {
	return m.intentSecret, m.intentID, nil
}

func (m *mockStripeService) CreateSubscription(customerID, priceID string) (string, error) {
	return "sub_test123", nil
}

func (m *mockStripeService) CancelSubscription(subscriptionID string) error {
	return nil
}

func (m *mockStripeService) CreateTransfer(amountCents int64, destination string) (string, error) {
	return m.transferID, nil
}

func (m *mockStripeService) VerifyWebhook(payload []byte, sigHeader string) (billing.WebhookEvent, error) {
	return billing.WebhookEvent{Type: "payment_intent.succeeded", PaymentIntentID: "pi_test", AmountCents: 10000}, nil
}

func TestMockStripeService_CreateOrGetCustomer(t *testing.T) {
	svc := &mockStripeService{customerID: "cus_test123"}
	id, err := svc.CreateOrGetCustomer("user@example.com")
	require.NoError(t, err)
	assert.Equal(t, "cus_test123", id)
}

func TestMockStripeService_CreatePaymentIntent(t *testing.T) {
	svc := &mockStripeService{intentSecret: "pi_secret_abc", intentID: "pi_abc"}
	secret, id, err := svc.CreatePaymentIntent(10000, "cus_123", "gopedia")
	require.NoError(t, err)
	assert.Equal(t, "pi_secret_abc", secret)
	assert.Equal(t, "pi_abc", id)
}

func TestStripeClientInit(t *testing.T) {
	// 실제 Stripe 클라이언트는 test key로 초기화 가능
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"cus_test","object":"customer"}`))
	}))
	defer srv.Close()

	// StripeClient가 올바르게 생성되는지만 확인
	client := billing.NewStripeClient("sk_test_abc", "price_test", "whsec_test")
	assert.NotNil(t, client)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/billing/... -run TestMock -v
go test ./internal/billing/... -run TestStripeClient -v
```
Expected: `FAIL` (StripeService 인터페이스, WebhookEvent 타입 없음)

- [ ] **Step 3: service.go 구현**

`internal/billing/service.go`:

```go
package billing

import (
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/paymentintent"
	stripesubscription "github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/transfer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// WebhookEvent는 Stripe 웹훅에서 파싱된 핵심 정보입니다.
type WebhookEvent struct {
	Type            string
	PaymentIntentID string
	AmountCents     int64
	CustomerID      string
	SubscriptionID  string
	Metadata        map[string]string
}

// StripeService는 Stripe API 호출을 추상화합니다.
type StripeService interface {
	CreateOrGetCustomer(email string) (customerID string, err error)
	CreatePaymentIntent(amountCents int64, customerID, packageSlug string) (clientSecret, intentID string, err error)
	CreateSubscription(customerID, priceID string) (subscriptionID string, err error)
	CancelSubscription(subscriptionID string) error
	CreateTransfer(amountCents int64, destination string) (transferID string, err error)
	VerifyWebhook(payload []byte, sigHeader string) (WebhookEvent, error)
}

// StripeClient는 실제 Stripe API를 호출하는 구현체입니다.
type StripeClient struct {
	proPriceID    string
	webhookSecret string
}

func NewStripeClient(secretKey, proPriceID, webhookSecret string) *StripeClient {
	stripe.Key = secretKey
	return &StripeClient{
		proPriceID:    proPriceID,
		webhookSecret: webhookSecret,
	}
}

func (c *StripeClient) CreateOrGetCustomer(email string) (string, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}
	result, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}
	return result.ID, nil
}

func (c *StripeClient) CreatePaymentIntent(amountCents int64, customerID, packageSlug string) (string, string, error) {
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amountCents),
		Currency: stripe.String(string(stripe.CurrencyKRW)),
		Customer: stripe.String(customerID),
		Metadata: map[string]string{
			"package_slug": packageSlug,
		},
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}
	result, err := paymentintent.New(params)
	if err != nil {
		return "", "", fmt.Errorf("create payment intent: %w", err)
	}
	return result.ClientSecret, result.ID, nil
}

func (c *StripeClient) CreateSubscription(customerID, priceID string) (string, error) {
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: stripe.String(priceID)},
		},
	}
	result, err := stripesubscription.New(params)
	if err != nil {
		return "", fmt.Errorf("create subscription: %w", err)
	}
	return result.ID, nil
}

func (c *StripeClient) CancelSubscription(subscriptionID string) error {
	_, err := stripesubscription.Cancel(subscriptionID, nil)
	if err != nil {
		return fmt.Errorf("cancel subscription: %w", err)
	}
	return nil
}

func (c *StripeClient) CreateTransfer(amountCents int64, destination string) (string, error) {
	params := &stripe.TransferParams{
		Amount:      stripe.Int64(amountCents),
		Currency:    stripe.String(string(stripe.CurrencyKRW)),
		Destination: stripe.String(destination),
	}
	result, err := transfer.New(params)
	if err != nil {
		return "", fmt.Errorf("create transfer: %w", err)
	}
	return result.ID, nil
}

func (c *StripeClient) VerifyWebhook(payload []byte, sigHeader string) (WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, c.webhookSecret)
	if err != nil {
		return WebhookEvent{}, fmt.Errorf("verify webhook: %w", err)
	}

	ev := WebhookEvent{Type: string(event.Type)}

	switch event.Type {
	case "payment_intent.succeeded":
		var pi stripe.PaymentIntent
		if err := event.GetObjectAs(&pi); err != nil {
			return WebhookEvent{}, fmt.Errorf("parse payment intent: %w", err)
		}
		ev.PaymentIntentID = pi.ID
		ev.AmountCents = pi.Amount
		ev.CustomerID = pi.Customer.ID
		ev.Metadata = pi.Metadata
	case "customer.subscription.created", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := event.GetObjectAs(&sub); err != nil {
			return WebhookEvent{}, fmt.Errorf("parse subscription: %w", err)
		}
		ev.SubscriptionID = sub.ID
		ev.CustomerID = sub.Customer.ID
	}

	return ev, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/billing/... -v -race
```
Expected: 전체 PASS (domain 4 + store 4 + service 3)

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/billing/service.go internal/billing/service_test.go
git commit -m "feat: add Stripe service interface and client implementation"
git push origin main
```

---

## Task 5: Billing HTTP 핸들러

**Files:**
- Create: `internal/api/handlers/billing.go`
- Create: `internal/api/handlers/billing_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/api/handlers/billing_test.go`:

```go
package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/billing"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

// --- mock store ---
type mockBillingStore struct {
	pkgPrices map[string]int64
	purchases []*billing.PackagePurchase
}

func newMockBillingStore() *mockBillingStore {
	return &mockBillingStore{pkgPrices: map[string]int64{"gopedia": 10000}}
}

func (m *mockBillingStore) GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error) {
	return nil, billing.ErrNotFound
}
func (m *mockBillingStore) UpsertSubscription(ctx context.Context, sub *billing.Subscription) error {
	return nil
}
func (m *mockBillingStore) GetPackagePriceCents(ctx context.Context, slug string) (int64, error) {
	p, ok := m.pkgPrices[slug]
	if !ok {
		return 0, billing.ErrNotFound
	}
	return p, nil
}
func (m *mockBillingStore) RecordPurchase(ctx context.Context, purchase *billing.PackagePurchase) error {
	m.purchases = append(m.purchases, purchase)
	return nil
}
func (m *mockBillingStore) HasPurchased(ctx context.Context, buyerID, slug string) (bool, error) {
	return false, nil
}
func (m *mockBillingStore) RecordEarnings(ctx context.Context, e *billing.PublisherEarnings) error {
	return nil
}
func (m *mockBillingStore) GetPendingEarnings(ctx context.Context, publisherID string) (int64, error) {
	return 9900, nil
}
func (m *mockBillingStore) RecordPayout(ctx context.Context, payout *billing.Payout) error {
	return nil
}
func (m *mockBillingStore) MarkEarningsPaidOut(ctx context.Context, publisherID, payoutID string) error {
	return nil
}

// --- mock stripe service ---
type mockStripe struct{}

func (m *mockStripe) CreateOrGetCustomer(email string) (string, error) { return "cus_test", nil }
func (m *mockStripe) CreatePaymentIntent(amountCents int64, customerID, slug string) (string, string, error) {
	return "pi_secret_test", "pi_test123", nil
}
func (m *mockStripe) CreateSubscription(customerID, priceID string) (string, error) {
	return "sub_test", nil
}
func (m *mockStripe) CancelSubscription(subscriptionID string) error { return nil }
func (m *mockStripe) CreateTransfer(amountCents int64, destination string) (string, error) {
	return "tr_test", nil
}
func (m *mockStripe) VerifyWebhook(payload []byte, sigHeader string) (billing.WebhookEvent, error) {
	return billing.WebhookEvent{
		Type:            "payment_intent.succeeded",
		PaymentIntentID: "pi_test123",
		AmountCents:     10000,
		CustomerID:      "cus_test",
		Metadata:        map[string]string{"package_slug": "gopedia", "version": "1.0.0", "buyer_id": "user-1", "publisher_id": "pub-1"},
	}, nil
}

// helper: inject claims into request context
func withClaims(r *http.Request, claims *domain.UserClaims) *http.Request {
	return r.WithContext(auth.WithClaims(r.Context(), claims))
}

func TestBillingHandler_CreatePaymentIntent(t *testing.T) {
	h := handlers.NewBillingHandler(newMockBillingStore(), &mockStripe{}, "price_pro_test")

	body, _ := json.Marshal(map[string]string{"package_slug": "gopedia", "version": "1.0.0", "publisher_id": "pub-1"})
	r := httptest.NewRequest(http.MethodPost, "/packages/gopedia/purchase", bytes.NewReader(body))
	r = withClaims(r, &domain.UserClaims{UserID: "user-1", Email: "u@test.com", Tier: domain.TierFree})
	rr := httptest.NewRecorder()

	h.CreatePaymentIntent(rr, r)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp billing.CreatePaymentIntentResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "pi_secret_test", resp.ClientSecret)
	assert.Equal(t, int64(10000), resp.AmountCents)
}

func TestBillingHandler_GetEarnings(t *testing.T) {
	h := handlers.NewBillingHandler(newMockBillingStore(), &mockStripe{}, "price_pro_test")

	r := httptest.NewRequest(http.MethodGet, "/publishers/me/earnings", nil)
	r = withClaims(r, &domain.UserClaims{UserID: "pub-1", Email: "p@test.com", Tier: domain.TierPro})
	rr := httptest.NewRecorder()

	h.GetEarnings(rr, r)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]int64
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, int64(9900), resp["pending_net_cents"])
}

func TestBillingHandler_StripeWebhook(t *testing.T) {
	h := handlers.NewBillingHandler(newMockBillingStore(), &mockStripe{}, "price_pro_test")

	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{}`)))
	r.Header.Set("Stripe-Signature", "t=123,v1=abc")
	rr := httptest.NewRecorder()

	h.StripeWebhook(rr, r)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestBillingHandler_CreatePaymentIntent_Unauthenticated(t *testing.T) {
	h := handlers.NewBillingHandler(newMockBillingStore(), &mockStripe{}, "price_pro_test")

	body, _ := json.Marshal(map[string]string{"package_slug": "gopedia"})
	r := httptest.NewRequest(http.MethodPost, "/packages/gopedia/purchase", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.CreatePaymentIntent(rr, r)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestBilling -v
```
Expected: `FAIL` (handlers.BillingHandler 없음)

- [ ] **Step 3: billing.go 핸들러 구현**

`internal/api/handlers/billing.go`:

```go
package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/billing"
)

type BillingHandler struct {
	store      billing.Store
	stripe     billing.StripeService
	proPriceID string
}

func NewBillingHandler(store billing.Store, stripe billing.StripeService, proPriceID string) *BillingHandler {
	return &BillingHandler{store: store, stripe: stripe, proPriceID: proPriceID}
}

type purchaseRequest struct {
	PackageSlug string `json:"package_slug"`
	Version     string `json:"version"`
	PublisherID string `json:"publisher_id"`
}

// CreatePaymentIntent handles POST /packages/{slug}/purchase
func (h *BillingHandler) CreatePaymentIntent(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req purchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	amountCents, err := h.store.GetPackagePriceCents(r.Context(), req.PackageSlug)
	if err != nil {
		http.Error(w, "package not found", http.StatusNotFound)
		return
	}
	customerID, err := h.stripe.CreateOrGetCustomer(claims.Email)
	if err != nil {
		http.Error(w, "payment setup failed", http.StatusInternalServerError)
		return
	}
	clientSecret, _, err := h.stripe.CreatePaymentIntent(amountCents, customerID, req.PackageSlug)
	if err != nil {
		http.Error(w, "payment intent failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(billing.CreatePaymentIntentResponse{
		ClientSecret: clientSecret,
		AmountCents:  amountCents,
	})
}

// GetEarnings handles GET /publishers/me/earnings
func (h *BillingHandler) GetEarnings(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	total, err := h.store.GetPendingEarnings(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"pending_net_cents": total})
}

type payoutRequest struct {
	StripeConnectAccountID string `json:"stripe_connect_account_id"`
}

// RequestPayout handles POST /publishers/me/payouts
func (h *BillingHandler) RequestPayout(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req payoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.StripeConnectAccountID == "" {
		http.Error(w, "stripe_connect_account_id required", http.StatusBadRequest)
		return
	}
	total, err := h.store.GetPendingEarnings(r.Context(), claims.UserID)
	if err != nil || total == 0 {
		http.Error(w, "no pending earnings", http.StatusBadRequest)
		return
	}
	transferID, err := h.stripe.CreateTransfer(total, req.StripeConnectAccountID)
	if err != nil {
		http.Error(w, "transfer failed", http.StatusInternalServerError)
		return
	}
	payout := &billing.Payout{
		PublisherID:      claims.UserID,
		AmountCents:      total,
		StripeTransferID: transferID,
	}
	if err := h.store.RecordPayout(r.Context(), payout); err != nil {
		http.Error(w, "record payout failed", http.StatusInternalServerError)
		return
	}
	if err := h.store.MarkEarningsPaidOut(r.Context(), claims.UserID, payout.ID); err != nil {
		http.Error(w, "mark paid out failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payout)
}

// StripeWebhook handles POST /webhooks/stripe
func (h *BillingHandler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	event, err := h.stripe.VerifyWebhook(payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "payment_intent.succeeded":
		h.handlePaymentSuccess(w, r, event)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (h *BillingHandler) handlePaymentSuccess(w http.ResponseWriter, r *http.Request, event billing.WebhookEvent) {
	purchase := &billing.PackagePurchase{
		BuyerID:               event.Metadata["buyer_id"],
		PackageSlug:           event.Metadata["package_slug"],
		Version:               event.Metadata["version"],
		AmountCents:           event.AmountCents,
		StripePaymentIntentID: event.PaymentIntentID,
	}
	if err := h.store.RecordPurchase(r.Context(), purchase); err != nil {
		http.Error(w, "record purchase failed", http.StatusInternalServerError)
		return
	}
	fee, net := billing.CalculateEarnings(event.AmountCents)
	earnings := &billing.PublisherEarnings{
		PublisherID:       event.Metadata["publisher_id"],
		PackagePurchaseID: purchase.ID,
		GrossCents:        event.AmountCents,
		FeeCents:          fee,
		NetCents:          net,
		Status:            billing.StatusPending,
	}
	if err := h.store.RecordEarnings(r.Context(), earnings); err != nil {
		http.Error(w, "record earnings failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -v -race
```
Expected: 기존 2개 + 신규 4개 = 6개 PASS

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/api/handlers/billing.go internal/api/handlers/billing_test.go
git commit -m "feat: add billing HTTP handlers (purchase, earnings, payout, webhook)"
git push origin main
```

---

## Task 6: Config 업데이트 & 라우터 연결

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: config.go 수정 — Stripe 필드 추가**

`internal/config/config.go` 전체 교체:

```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	RedisURL            string
	ZitadelIssuer       string
	ArtifactKeeperURL   string
	ArtifactKeeperToken string
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeProPriceID    string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	issuer := os.Getenv("ZITADEL_ISSUER")
	if issuer == "" {
		return nil, fmt.Errorf("ZITADEL_ISSUER is required")
	}
	akURL := os.Getenv("ARTIFACT_KEEPER_URL")
	if akURL == "" {
		return nil, fmt.Errorf("ARTIFACT_KEEPER_URL is required")
	}
	akToken := os.Getenv("ARTIFACT_KEEPER_TOKEN")
	if akToken == "" {
		return nil, fmt.Errorf("ARTIFACT_KEEPER_TOKEN is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return &Config{
		Port:                port,
		DatabaseURL:         dbURL,
		RedisURL:            os.Getenv("REDIS_URL"),
		ZitadelIssuer:       issuer,
		ArtifactKeeperURL:   akURL,
		ArtifactKeeperToken: akToken,
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeProPriceID:    os.Getenv("STRIPE_PRO_PRICE_ID"),
	}, nil
}
```

- [ ] **Step 2: 기존 config 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/config/... -v -race
```
Expected: 3개 PASS (Stripe 필드는 선택사항이므로 기존 테스트 그대로 통과)

- [ ] **Step 3: router.go 수정 — billing 라우트 추가**

`internal/api/router.go` 전체 교체:

```go
package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/billing"
	"github.com/tojiuni/morphso-hub/internal/recommend"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

func NewRouter(
	pool *pgxpool.Pool,
	store registry.Store,
	authMW *auth.Middleware,
	engine *recommend.Engine,
	billingStore billing.Store,
	stripeService billing.StripeService,
	stripeProPriceID string,
) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	pkgHandler := handlers.NewPackageHandler(store)
	recHandler := handlers.NewRecommendHandler(engine)
	instHandler := handlers.NewInstallHandler(pool)
	billHandler := handlers.NewBillingHandler(billingStore, stripeService, stripeProPriceID)

	r.Get("/health", handlers.Health)

	// Stripe webhook은 서명 검증을 위해 raw body가 필요 — 인증 미들웨어 없이 등록
	r.Post("/webhooks/stripe", billHandler.StripeWebhook)

	r.Group(func(r chi.Router) {
		r.Use(authMW.OptionalAuth)
		r.Get("/packages", pkgHandler.Search)
		r.Get("/packages/{slug}", pkgHandler.GetBySlug)
	})

	r.Group(func(r chi.Router) {
		r.Use(authMW.RequireAuth)
		r.Post("/packages", pkgHandler.Create)
		r.Post("/recommend", recHandler.Recommend)
		r.Post("/installs", instHandler.RecordInstall)
		r.Get("/users/me/installs", instHandler.ListMyInstalls)

		// Billing
		r.Post("/packages/{slug}/purchase", billHandler.CreatePaymentIntent)
		r.Get("/publishers/me/earnings", billHandler.GetEarnings)
		r.Post("/publishers/me/payouts", billHandler.RequestPayout)
	})

	return r
}
```

- [ ] **Step 4: main.go 수정 — billing 의존성 조립**

`cmd/server/main.go` 전체 교체:

```go
package main

import (
	"context"
	"log"
	"net/http"

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

	if err := db.RunMigrations(cfg.DatabaseURL, "internal/db/migrations"); err != nil {
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
	stripeService := billingpkg.NewStripeClient(cfg.StripeSecretKey, cfg.StripeProPriceID, cfg.StripeWebhookSecret)

	router := api.NewRouter(pool, store, authMW, engine, billingStore, stripeService, cfg.StripeProPriceID)

	log.Printf("morphso-hub listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 5: 전체 빌드 & 테스트**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go build ./...
go test ./... -v -race
```
Expected: 전체 빌드 성공, 모든 테스트 PASS

- [ ] **Step 6: .env.example 업데이트**

`.env.example` 맨 아래에 추가:

```bash
STRIPE_SECRET_KEY=sk_test_<your_key>
STRIPE_WEBHOOK_SECRET=whsec_<your_secret>
STRIPE_PRO_PRICE_ID=price_<your_price_id>
```

- [ ] **Step 7: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/config/config.go internal/api/router.go cmd/server/main.go .env.example
git commit -m "feat: wire billing into router and config"
git push origin main
```

---

## 스펙 자체 검토

**스펙 커버리지:**
- ✅ subscriptions 테이블 → Task 1
- ✅ package_purchases 테이블 → Task 1
- ✅ publisher_earnings 테이블 (1% 수수료) → Task 1, 2
- ✅ payouts 테이블 → Task 1
- ✅ Stripe 구독 (Pro tier) → Task 4
- ✅ 유료 패키지 구매 (PaymentIntent) → Task 4, 5
- ✅ 수수료 1% 계산 → Task 2 (CalculateEarnings)
- ✅ 퍼블리셔 정산 (Stripe Connect Transfer) → Task 4, 5
- ✅ Stripe webhook 처리 → Task 5
- ✅ 라우터 연결 → Task 6

**다음 계획:**
- **Plan 3**: morphso CLI — Go 단일 바이너리, OS 스펙 수집, AI 추천 연동, install strategy 실행
- **Plan 4**: neunexus K8s 배포 — morphso ns, Vault 시크릿, Traefik IngressRoute
