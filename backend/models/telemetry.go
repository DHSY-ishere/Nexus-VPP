package models

import "time"

// TelemetryEvent represents a single ping from a Contributor, Consumer, or Prosumer node
type TelemetryEvent struct {
	NodeID             string    `json:"node_id"`
	NodeType           string    `json:"node_type"` // "CONTRIBUTOR", "CONSUMER", "PROSUMER"
	Timestamp          time.Time `json:"timestamp"`
	
	// Core VPP Metrics
	CurrentConsumption float64   `json:"current_consumption_kw"`
	ReplenishmentRate  float64   `json:"replenishment_rate_kw"` 
	StateOfCharge      float64   `json:"state_of_charge_percent"` // Battery level (0-100)
	
	// Grid Health Indicators
	LocalFrequency     float64   `json:"local_frequency_hz"` // Targeting 50.0Hz
	Voltage            float64   `json:"voltage_v"`
}