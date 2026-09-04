package models

import "time"

// Order ساختار فاکتور در دیتابیس را مشخص می‌کند
type Order struct {
	ID           int       `json:"id"`
	TrackingCode string    `json:"tracking_code"`
	PlanName     string    `json:"plan_name"`
	BasePrice    int       `json:"base_price"`
	UniqueAmount int       `json:"unique_amount"`
	Status       string    `json:"status"` // pending یا paid
	ConfigLink   string    `json:"config_link"`
	CreatedAt    time.Time `json:"created_at"`
	RenewUsername string `json:"renew_username"`
	CarryGB       int    `json:"carry_gb"`
}
