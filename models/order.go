package models

import (
	"encoding/json"
	"os"
)

type Order struct {
	ID             int    `json:"id"`
	TrackingCode   string `json:"tracking_code"`
	PlanName       string `json:"plan_name"`
	BasePrice      int    `json:"base_price"`
	UniqueAmount   int    `json:"unique_amount"`
	Status         string `json:"status"`
	ConfigLink     string `json:"config_link"`
	AdminConfirmed bool   `json:"admin_confirmed"`
	PaymentMethod  string `json:"payment_method"`
	CreatedAt      string `json:"created_at"`
	PaidAt         string `json:"paid_at"`
	AdminNote      string `json:"admin_note"`
	Archived       int    `json:"archived"`
	PlanDays       int    `json:"plan_days"`
	RenewUsername  string `json:"renew_username"`
	CarryGB        int    `json:"carry_gb"`
}

// LoadPlans خواندن پلن‌ها از فایل
func LoadPlans() ([]Plan, error) {
	data, err := os.ReadFile("plans.json")
	if err != nil {
		return nil, err
	}
	var plans []Plan
	err = json.Unmarshal(data, &plans)
	return plans, err
}
