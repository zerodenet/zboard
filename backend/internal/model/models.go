package model

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Username  string         `json:"username" gorm:"size:64;uniqueIndex;not null"`
	Email     string         `json:"email" gorm:"size:128;uniqueIndex;not null"`
	Password  string         `json:"-" gorm:"size:255;not null"`
	IsAdmin   bool           `json:"is_admin" gorm:"default:false"`
	Status    string         `json:"status" gorm:"size:20;default:active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type Plan struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:64;uniqueIndex"`
	PriceCents  int64     `json:"price_cents" gorm:"not null"`
	TrafficGB   int64     `json:"traffic_gb" gorm:"not null"`
	DurationDay int       `json:"duration_day" gorm:"not null"`
	MaxDevice   int       `json:"max_device"`
	IsActive    bool      `json:"is_active" gorm:"default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Subscription struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`
	PlanID    uint      `json:"plan_id" gorm:"index"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	Status    string    `json:"status" gorm:"size:20;default:active"`
	FlowTotal int64     `json:"flow_total" gorm:"default:0"`
	FlowUsed  int64     `json:"flow_used" gorm:"default:0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Order struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	UserID         uint      `json:"user_id" gorm:"index"`
	SubscriptionID uint      `json:"subscription_id" gorm:"index"`
	PlanID         uint      `json:"plan_id" gorm:"index"`
	TradeNo        string    `json:"trade_no" gorm:"size:64;uniqueIndex"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency" gorm:"size:16;default:USD"`
	Channel        string    `json:"channel" gorm:"size:32"`
	Status         string    `json:"status" gorm:"size:32;default:pending"`
	RawCallback    string    `json:"raw_callback" gorm:"type:text"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Node struct {
	ID                     uint       `json:"id" gorm:"primaryKey"`
	Name                   string     `json:"name" gorm:"size:64;uniqueIndex"`
	Region                 string     `json:"region" gorm:"size:32"`
	Address                string     `json:"address" gorm:"size:128"`
	Protocol               string     `json:"protocol" gorm:"size:32"`
	IsOnline               bool       `json:"is_online" gorm:"default:false"`
	LastSeenAt             *time.Time `json:"last_seen_at"`
	SSHHost                string     `json:"ssh_host" gorm:"size:64"`
	SSHPort                int        `json:"ssh_port" gorm:"default:22"`
	SSHUser                string     `json:"ssh_user" gorm:"size:64"`
	SSHPwd                 string     `json:"-" gorm:"column:ssh_pwd;type:text"`
	SSHHostKeyFingerprint  string     `json:"ssh_host_key_fingerprint" gorm:"size:128"`
	TrafficSecret          string     `json:"-" gorm:"column:traffic_secret;type:text"`
	TrafficSecretPrefix    string     `json:"traffic_secret_prefix,omitempty" gorm:"size:12"`
	TrafficSecretRevokedAt *time.Time `json:"traffic_secret_revoked_at,omitempty"`
	ProtocolConfig         string     `json:"-" gorm:"type:text"`
	ClientConfig           string     `json:"client_config,omitempty" gorm:"type:text"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SubscriptionToken struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	UserID      uint       `json:"user_id" gorm:"uniqueIndex;not null"`
	TokenHash   string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"size:12;not null"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TrafficRecord struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`
	NodeID    uint      `json:"node_id" gorm:"index;uniqueIndex:ux_traffic_node_report,priority:1;uniqueIndex:ux_traffic_node_nonce,priority:1"`
	ReportID  string    `json:"report_id,omitempty" gorm:"size:64;uniqueIndex:ux_traffic_node_report,priority:2"`
	Nonce     string    `json:"-" gorm:"size:64;uniqueIndex:ux_traffic_node_nonce,priority:2"`
	UsedBytes int64     `json:"used_bytes"`
	At        time.Time `json:"record_at" gorm:"index;column:record_at"`
	Meta      string    `json:"meta" gorm:"type:text"`
}

type AuditLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`
	Actor     string    `json:"actor" gorm:"size:64"`
	Action    string    `json:"action" gorm:"size:128"`
	Target    string    `json:"target" gorm:"size:128"`
	Detail    string    `json:"detail" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}
