package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PaymentOrder struct {
	ID             string         `json:"id"`
	OrderType      string         `json:"orderType"`
	PlanCode       string         `json:"planCode"`
	RechargeAmount float64        `json:"rechargeAmount"`
	AmountCents    int            `json:"amountCents"`
	Currency       string         `json:"currency"`
	SubMethod      string         `json:"subMethod"`
	Status         string         `json:"status"`
	Description    string         `json:"description"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

func (s *Store) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT code, name, tier, price_cents, interval, description, features
		FROM plans
		WHERE active = TRUE
		ORDER BY price_cents ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var plan Plan
		var featuresRaw []byte
		if err := rows.Scan(&plan.Code, &plan.Name, &plan.Tier, &plan.PriceCents, &plan.Interval, &plan.Description, &featuresRaw); err != nil {
			return nil, err
		}
		plan.Features = unmarshalStrings(featuresRaw)
		out = append(out, plan)
	}
	return out, rows.Err()
}

func (s *Store) GetPlan(ctx context.Context, code string) (*Plan, error) {
	var plan Plan
	var featuresRaw []byte
	err := s.DB.QueryRow(ctx, `
		SELECT code, name, tier, price_cents, interval, description, features
		FROM plans
		WHERE code = $1
	`, code).Scan(&plan.Code, &plan.Name, &plan.Tier, &plan.PriceCents, &plan.Interval, &plan.Description, &featuresRaw)
	if err != nil {
		return nil, scanNotFound(err)
	}
	plan.Features = unmarshalStrings(featuresRaw)
	return &plan, nil
}

func (s *Store) GetSubscriptionByUser(ctx context.Context, userID string) (*Subscription, error) {
	var sub Subscription
	var endsAt *time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, plan_code, status, started_at, ends_at, auto_renew
		FROM subscriptions
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&sub.ID, &sub.PlanCode, &sub.Status, &sub.StartedAt, &endsAt, &sub.AutoRenew)
	if err != nil {
		return nil, scanNotFound(err)
	}
	sub.EndsAt = endsAt
	return &sub, nil
}

func (s *Store) UpsertSubscription(ctx context.Context, userID string, planCode string, endsAt *time.Time, source string, sourceReference string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO subscriptions (user_id, plan_code, status, started_at, ends_at, auto_renew, source, source_reference)
		VALUES ($1, $2, 'active', NOW(), $3, TRUE, $4, $5)
	`, userID, planCode, endsAt, source, sourceReference)
	return err
}

func (s *Store) CreatePaymentOrder(ctx context.Context, userID string, orderType string, planCode string, rechargeAmount float64, amountCents int, description string, subMethod string, metadata map[string]any) (*PaymentOrder, error) {
	var order PaymentOrder
	var metadataRaw []byte
	err := s.DB.QueryRow(ctx, `
		INSERT INTO payment_orders (user_id, order_type, plan_code, recharge_amount, amount_cents, description, sub_method, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, 'pending')
		RETURNING id::text, order_type, plan_code, recharge_amount, amount_cents, currency, sub_method, status, description, metadata, created_at, updated_at
	`, userID, orderType, planCode, rechargeAmount, amountCents, description, subMethod, marshalJSON(metadata)).Scan(
		&order.ID, &order.OrderType, &order.PlanCode, &order.RechargeAmount, &order.AmountCents, &order.Currency,
		&order.SubMethod, &order.Status, &order.Description, &metadataRaw, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadataRaw, &order.Metadata)
	return &order, nil
}

func (s *Store) GetPaymentOrder(ctx context.Context, userID string, orderID string) (*PaymentOrder, error) {
	var order PaymentOrder
	var metadataRaw []byte
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, order_type, plan_code, recharge_amount, amount_cents, currency, sub_method, status, description, metadata, created_at, updated_at
		FROM payment_orders
		WHERE id = $1 AND user_id = $2
	`, orderID, userID).Scan(&order.ID, &order.OrderType, &order.PlanCode, &order.RechargeAmount, &order.AmountCents, &order.Currency, &order.SubMethod, &order.Status, &order.Description, &metadataRaw, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	_ = json.Unmarshal(metadataRaw, &order.Metadata)
	return &order, nil
}

func (s *Store) GetPaymentOrderByID(ctx context.Context, orderID string) (*PaymentOrder, error) {
	var order PaymentOrder
	var metadataRaw []byte
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, order_type, plan_code, recharge_amount, amount_cents, currency, sub_method, status, description, metadata, created_at, updated_at
		FROM payment_orders
		WHERE id = $1
	`, orderID).Scan(&order.ID, &order.OrderType, &order.PlanCode, &order.RechargeAmount, &order.AmountCents, &order.Currency, &order.SubMethod, &order.Status, &order.Description, &metadataRaw, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	_ = json.Unmarshal(metadataRaw, &order.Metadata)
	return &order, nil
}

func (s *Store) UpdatePaymentOrderStatus(ctx context.Context, orderID string, status string, ifpayPaymentID string, ifpayOrderID string, payload map[string]any) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE payment_orders
		SET status = $2,
			ifpay_payment_id = COALESCE(NULLIF($3, ''), ifpay_payment_id),
			ifpay_order_id = COALESCE(NULLIF($4, ''), ifpay_order_id),
			metadata = COALESCE($5::jsonb, metadata),
			updated_at = NOW()
		WHERE id = $1
	`, orderID, status, ifpayPaymentID, ifpayOrderID, marshalJSON(payload))
	return err
}

func (s *Store) RecordPaymentEvent(ctx context.Context, orderID string, eventType string, payload map[string]any, signatureOK bool) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO payment_events (order_id, event_type, payload, signature_ok, processed_at)
		VALUES (NULLIF($1, '')::uuid, $2, $3::jsonb, $4, NOW())
	`, nullString(orderID), eventType, marshalJSON(payload), signatureOK)
	return err
}

func (s *Store) ApplySuccessfulPayment(ctx context.Context, orderID string, ifpayPaymentID string, ifpayOrderID string, payload map[string]any) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var order struct {
			ID             string
			UserID         string
			OrderType      string
			PlanCode       string
			RechargeAmount float64
			Status         string
		}
		err := tx.QueryRow(ctx, `
			SELECT id::text, user_id::text, order_type, plan_code, recharge_amount, status
			FROM payment_orders
			WHERE id = $1
			FOR UPDATE
		`, orderID).Scan(&order.ID, &order.UserID, &order.OrderType, &order.PlanCode, &order.RechargeAmount, &order.Status)
		if err != nil {
			return scanNotFound(err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payment_orders
			SET status = 'succeeded',
				ifpay_payment_id = COALESCE(NULLIF($2, ''), ifpay_payment_id),
				ifpay_order_id = COALESCE(NULLIF($3, ''), ifpay_order_id),
				metadata = COALESCE($4::jsonb, metadata),
				updated_at = NOW()
			WHERE id = $1
		`, order.ID, ifpayPaymentID, ifpayOrderID, marshalJSON(payload)); err != nil {
			return err
		}
		if order.Status == "succeeded" {
			return nil
		}

		switch strings.ToLower(strings.TrimSpace(order.OrderType)) {
		case "plan":
			return s.applyPlanPaymentTX(ctx, tx, order.UserID, order.PlanCode, order.ID)
		default:
			return s.applyRechargePaymentTX(ctx, tx, order.UserID, order.RechargeAmount, order.ID, payload)
		}
	})
}

func (s *Store) applyPlanPaymentTX(ctx context.Context, tx pgx.Tx, userID string, planCode string, referenceID string) error {
	var interval string
	if err := tx.QueryRow(ctx, `
		SELECT interval
		FROM plans
		WHERE code = $1
	`, planCode).Scan(&interval); err != nil {
		return scanNotFound(err)
	}

	now := s.now()
	var currentEndsAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT ends_at
		FROM subscriptions
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&currentEndsAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	base := now
	if currentEndsAt != nil && currentEndsAt.After(base) {
		base = *currentEndsAt
	}
	endsAt := subscriptionEndsAt(base, interval)

	if _, err := tx.Exec(ctx, `
		UPDATE subscriptions
		SET status = 'expired', auto_renew = FALSE, updated_at = NOW()
		WHERE user_id = $1 AND status = 'active'
	`, userID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO subscriptions (user_id, plan_code, status, started_at, ends_at, auto_renew, source, source_reference)
		VALUES ($1, $2, 'active', NOW(), $3, FALSE, 'payment', $4)
	`, userID, planCode, endsAt, referenceID)
	return err
}

func (s *Store) applyRechargePaymentTX(ctx context.Context, tx pgx.Tx, userID string, amount float64, referenceID string, metadata map[string]any) error {
	var current float64
	err := tx.QueryRow(ctx, `
		SELECT balance_after
		FROM quota_ledgers
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, userID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	next := current + amount
	_, err = tx.Exec(ctx, `
		INSERT INTO quota_ledgers (user_id, event_type, amount, balance_after, source, reference_id, metadata)
		VALUES ($1, 'payment_recharge', $2, $3, 'payment', $4, $5::jsonb)
	`, userID, amount, next, referenceID, marshalJSON(metadata))
	return err
}

func subscriptionEndsAt(base time.Time, interval string) *time.Time {
	normalized := strings.ToLower(strings.TrimSpace(interval))
	var next time.Time
	switch normalized {
	case "lifetime", "forever", "permanent":
		return nil
	case "year", "annual":
		next = base.AddDate(1, 0, 0)
	case "week", "weekly":
		next = base.AddDate(0, 0, 7)
	case "day", "daily":
		next = base.AddDate(0, 0, 1)
	default:
		next = base.AddDate(0, 1, 0)
	}
	return &next
}
