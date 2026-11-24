package api

const (
	Address            = "127.0.0.1:18765"
	Origin             = "http://" + Address
	MaxControlBody     = 1024
	MaxConnectionBody  = 8 << 10
	MaxTransactionBody = 1 << 20
)

type APIVersion struct {
	Major int `json:"major" example:"1"`
	Minor int `json:"minor" example:"12"`
}

type InfoResponse struct {
	Name         string     `json:"name" example:"wallet-bridge"`
	Version      string     `json:"version" example:"dev"`
	APIVersion   APIVersion `json:"apiVersion"`
	Capabilities []string   `json:"capabilities" example:"service.status,service.stop,browser.authorization,service.auth.shared,connections.lifecycle,browser.connections.coordination,browser.connections.decisions,browser.wallet-operation,transactions.lifecycle,browser.transactions.read,browser.transactions.coordination,browser.transactions.decisions,browser.pending-requests"`
	Ready        bool       `json:"ready" example:"true"`
	Challenge    string     `json:"challenge,omitempty"`
	Proof        string     `json:"proof,omitempty"`
}

type WalletOperationResponse struct {
	ConnectionID string `json:"connectionId"`
	Phase        string `json:"phase" enums:"ready,calling,interrupted_calling"`
}

type ActiveWalletRequestResponse struct {
	Kind  string `json:"kind" enums:"connection,transaction"`
	ID    string `json:"id"`
	Phase string `json:"phase" enums:"ready,calling,interrupted_calling"`
}

type StatusResponse struct {
	ActiveWalletRequest *ActiveWalletRequestResponse `json:"activeWalletRequest,omitempty"`
	WalletOperation     *WalletOperationResponse     `json:"walletOperation,omitempty"`
	State               string                       `json:"state" example:"running"`
	StartedAt           string                       `json:"startedAt" format:"date-time"`
	Info                InfoResponse                 `json:"info"`
	Browser             *BrowserGrantResponse        `json:"browser,omitempty"`
}

type StopResponse struct {
	State string `json:"state" example:"stopping"`
}

type ErrorResponse struct {
	Code    string `json:"code" example:"unauthenticated"`
	Message string `json:"message" example:"A valid Bearer token is required"`
}

type BrowserTicketResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt" format:"date-time"`
}

type BrowserAuthorizeRequest struct {
	Ticket string `json:"ticket" minlength:"64" maxlength:"64"`
}

type BrowserGrantResponse struct {
	ID        string `json:"id"`
	IssuedAt  string `json:"issuedAt" format:"date-time"`
	ExpiresAt string `json:"expiresAt" format:"date-time"`
	RevokedAt string `json:"revokedAt,omitempty" format:"date-time"`
	State     string `json:"state" enums:"active,expired,revoked,not_yet_valid"`
}

type BrowserGrantListResponse struct {
	Grants    []BrowserGrantResponse `json:"grants"`
	NextAfter string                 `json:"nextAfter,omitempty"`
}

type BrowserRevokeRequest struct {
	ID  string `json:"id,omitempty"`
	All bool   `json:"all,omitempty"`
}

type BrowserRevokeResponse struct {
	State string `json:"state" example:"revoked"`
}
