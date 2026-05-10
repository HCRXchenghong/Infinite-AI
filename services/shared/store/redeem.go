package store

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type RedeemCampaignInput struct {
	Name        string `json:"name"`
	PlanCode    string `json:"planCode"`
	Duration    int    `json:"duration"`
	Lifetime    bool   `json:"lifetime"`
	AccountType string `json:"accountType"`
	MaxUses     int    `json:"maxUses"`
	ExpiryDate  string `json:"expiryDate"`
}

type RedeemResult struct {
	CampaignID string `json:"campaignId"`
	Code       string `json:"code"`
	Link       string `json:"link"`
}

func (s *Store) CreateRedeemCampaign(ctx context.Context, adminID string, input RedeemCampaignInput) (*RedeemResult, error) {
	if input.MaxUses <= 0 {
		input.MaxUses = 1
	}
	if input.Duration <= 0 && !input.Lifetime {
		input.Duration = 1
	}
	code := generateGiftCode()
	link := fmt.Sprintf("%s/redeem?code=%s", s.Config.RedeemBaseURL, code)
	var expiresAt *time.Time
	if input.ExpiryDate != "" {
		if parsed, err := time.Parse("2006-01-02", input.ExpiryDate); err == nil {
			expiresAt = &parsed
		}
	}
	var result RedeemResult
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var campaignID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO redeem_campaigns (name, plan_code, duration_months, lifetime, account_type, max_uses, expires_at, created_by_admin_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id::text
		`, input.Name, input.PlanCode, input.Duration, input.Lifetime, input.AccountType, input.MaxUses, expiresAt, adminID).Scan(&campaignID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO redeem_codes (campaign_id, code, gift_link, max_uses, remaining_uses, expires_at, status)
			VALUES ($1, $2, $3, $4, $4, $5, 'active')
		`, campaignID, code, link, input.MaxUses, expiresAt); err != nil {
			return err
		}
		result = RedeemResult{CampaignID: campaignID, Code: code, Link: link}
		return nil
	})
	return &result, err
}

func (s *Store) PreviewRedeemCode(ctx context.Context, code string) (*RedeemPreview, error) {
	var preview RedeemPreview
	var durationMonths int
	var lifetime bool
	var expiresAt *time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT rc.code, c.plan_code, p.name, c.duration_months, c.lifetime, c.account_type, rc.max_uses, rc.remaining_uses, rc.expires_at, rc.status
		FROM redeem_codes rc
		JOIN redeem_campaigns c ON c.id = rc.campaign_id
		JOIN plans p ON p.code = c.plan_code
		WHERE rc.code = $1
	`, strings.ToUpper(code)).Scan(&preview.Code, &preview.PlanCode, &preview.PlanName, &durationMonths, &lifetime, &preview.AccountType, &preview.MaxUses, &preview.Remaining, &expiresAt, &preview.Status)
	if err != nil {
		return nil, scanNotFound(err)
	}
	preview.ExpiresAt = expiresAt
	if lifetime {
		preview.DurationText = "永久有效"
	} else {
		preview.DurationText = fmt.Sprintf("%d 个月", durationMonths)
	}
	if preview.Status == "active" && preview.Remaining <= 0 {
		preview.Status = "exhausted"
	}
	if preview.Status == "active" && preview.ExpiresAt != nil && preview.ExpiresAt.Before(time.Now()) {
		preview.Status = "expired"
	}
	return &preview, nil
}

func (s *Store) ClaimRedeemCode(ctx context.Context, code string, userID string) (*RedeemPreview, error) {
	var preview *RedeemPreview
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var redeemCodeID string
		var planCode string
		var planName string
		var durationMonths int
		var lifetime bool
		var accountType string
		var maxUses int
		var remaining int
		var expiresAt *time.Time
		var status string
		err := tx.QueryRow(ctx, `
			SELECT rc.id::text, rc.code, c.plan_code, p.name, c.duration_months, c.lifetime, c.account_type, rc.max_uses, rc.remaining_uses, rc.expires_at, rc.status
			FROM redeem_codes rc
			JOIN redeem_campaigns c ON c.id = rc.campaign_id
			JOIN plans p ON p.code = c.plan_code
			WHERE rc.code = $1
			FOR UPDATE
		`, strings.ToUpper(code)).Scan(&redeemCodeID, &code, &planCode, &planName, &durationMonths, &lifetime, &accountType, &maxUses, &remaining, &expiresAt, &status)
		if err != nil {
			return err
		}
		if accountType == "has_account" && userID == "" {
			return fmt.Errorf("请先登录后再领取这个兑换码")
		}
		if status != "active" || remaining <= 0 || (expiresAt != nil && expiresAt.Before(time.Now())) {
			var claimExists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1
					FROM redeem_claims
					WHERE redeem_code_id = $1 AND user_id = $2
				)
			`, redeemCodeID, userID).Scan(&claimExists); err != nil {
				return err
			}
			if !claimExists {
				return fmt.Errorf("兑换码当前不可用")
			}
		}
		var claimExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM redeem_claims
				WHERE redeem_code_id = $1 AND user_id = $2
			)
		`, redeemCodeID, userID).Scan(&claimExists); err != nil {
			return err
		}
		if claimExists {
			preview = &RedeemPreview{
				Code:         code,
				PlanCode:     planCode,
				PlanName:     planName,
				AccountType:  accountType,
				MaxUses:      maxUses,
				Remaining:    remaining,
				ExpiresAt:    expiresAt,
				Status:       "claimed",
				DurationText: durationLabel(durationMonths, lifetime),
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO redeem_claims (redeem_code_id, user_id, claim_status, claimed_at, metadata)
			VALUES ($1, $2, 'claimed', NOW(), '{}'::jsonb)
		`, redeemCodeID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE redeem_codes
			SET remaining_uses = remaining_uses - 1
			WHERE id = $1 AND remaining_uses > 0
		`, redeemCodeID); err != nil {
			return err
		}
		var endsAt *time.Time
		if !lifetime {
			t := time.Now().AddDate(0, durationMonths, 0)
			endsAt = &t
		}
		if _, err := tx.Exec(ctx, `
			UPDATE subscriptions
			SET status = 'expired', auto_renew = FALSE, updated_at = NOW()
			WHERE user_id = $1 AND status = 'active'
		`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscriptions (user_id, plan_code, status, started_at, ends_at, auto_renew, source, source_reference)
			VALUES ($1, $2, 'active', NOW(), $3, FALSE, 'redeem', $4)
		`, userID, planCode, endsAt, redeemCodeID); err != nil {
			return err
		}
		preview = &RedeemPreview{
			Code:         code,
			PlanCode:     planCode,
			PlanName:     planName,
			AccountType:  accountType,
			MaxUses:      maxUses,
			Remaining:    remaining - 1,
			ExpiresAt:    expiresAt,
			Status:       "claimed",
			DurationText: durationLabel(durationMonths, lifetime),
		}
		return nil
	})
	return preview, err
}

func durationLabel(months int, lifetime bool) string {
	if lifetime {
		return "永久有效"
	}
	return fmt.Sprintf("%d 个月", months)
}

func generateGiftCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	builder := strings.Builder{}
	builder.WriteString("GIFT-")
	for i := 0; i < 8; i++ {
		builder.WriteByte(alphabet[rand.Intn(len(alphabet))])
	}
	return builder.String()
}
