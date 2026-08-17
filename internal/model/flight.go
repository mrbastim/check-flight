package model

// Flight is a normalized representation used across providers and the tracker core.
type Flight struct {
	UID       string
	Provider  string
	Direction string
	Code      string
	City      string
	SchedTime string
	Status    string
	Gate      string
	Terminal  string
}

// Query contains common provider-agnostic filters.
type Query struct {
	Direction string
	Search    string
	Terminal  string
}
