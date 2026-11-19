package api

const (
	Address        = "127.0.0.1:18765"
	Origin         = "http://" + Address
	MaxControlBody = 1024
)

type APIVersion struct {
	Major int `json:"major" example:"1"`
	Minor int `json:"minor" example:"0"`
}

type InfoResponse struct {
	Name         string     `json:"name" example:"wallet-bridge"`
	Version      string     `json:"version" example:"dev"`
	APIVersion   APIVersion `json:"apiVersion"`
	Capabilities []string   `json:"capabilities" example:"service.status,service.stop"`
	Ready        bool       `json:"ready" example:"true"`
	Challenge    string     `json:"challenge,omitempty"`
	Proof        string     `json:"proof,omitempty"`
}

type StatusResponse struct {
	State     string       `json:"state" example:"running"`
	StartedAt string       `json:"startedAt" format:"date-time"`
	Info      InfoResponse `json:"info"`
}

type StopResponse struct {
	State string `json:"state" example:"stopping"`
}

type ErrorResponse struct {
	Code    string `json:"code" example:"unauthenticated"`
	Message string `json:"message" example:"A valid Bearer token is required"`
}
