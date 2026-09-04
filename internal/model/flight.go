package model

// Flight is a normalized representation used across providers and the tracker core.
type Flight struct {
	UID                string
	InternalID         string
	Provider           string
	Direction          string
	Code               string
	City               string
	SchedTime          string
	EstimatedTime      string
	Status             string
	Gate               string
	Terminal           string
	BaggageBelt        string
	CheckInDesk        string
	GateChanged        bool
	BaggageBeltChanged bool
	CheckInDeskChanged bool
}

// Query contains common provider-agnostic filters.
type Query struct {
	Direction string
	Search    string
	Terminal  string
}
