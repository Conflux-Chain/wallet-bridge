package api

import "github.com/Conflux-Chain/wallet-bridge/internal/app"

type ConnectionCreateRequest struct {
	ID         string `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	ClientName string `json:"clientName" validate:"required" example:"Example CLI"`
	Intent     string `json:"intent,omitempty"`
	ChainID    string `json:"chainId" validate:"required" example:"71"`
	ChainName  string `json:"chainName,omitempty"`
}

type ConnectionResponse struct {
	ID         string `json:"id"`
	ClientName string `json:"clientName"`
	Intent     string `json:"intent"`
	ChainID    string `json:"chainId" example:"0x47"`
	ChainName  string `json:"chainName,omitempty"`
	State      string `json:"state" enums:"pending,succeeded,rejected,canceled,expired"`
	CreatedAt  string `json:"createdAt" format:"date-time"`
	ExpiresAt  string `json:"expiresAt" format:"date-time"`
	FinishedAt string `json:"finishedAt,omitempty" format:"date-time"`
	SessionID  string `json:"sessionId,omitempty"`
}

type WalletResponse struct {
	Kind string `json:"kind" enums:"eip6963,legacy"`
	Name string `json:"name"`
	RDNS string `json:"rdns,omitempty"`
}

type SessionResponse struct {
	ID           string         `json:"id"`
	ConnectionID string         `json:"connectionId"`
	Wallet       WalletResponse `json:"wallet"`
	Account      string         `json:"account"`
	ChainID      string         `json:"chainId" example:"0x47"`
	State        string         `json:"state" enums:"active,closed"`
	CreatedAt    string         `json:"createdAt" format:"date-time"`
	LastAccessAt string         `json:"lastAccessAt" format:"date-time"`
	ClosedAt     string         `json:"closedAt,omitempty" format:"date-time"`
}

func connectionResponse(request app.ConnectionRequest) ConnectionResponse {
	result := ConnectionResponse{
		ID: request.ID, ClientName: request.ClientName, Intent: request.Intent, ChainID: request.ChainID, ChainName: request.ChainName,
		State: string(request.State), CreatedAt: timestamp(request.CreatedAt), ExpiresAt: timestamp(request.ExpiresAt), SessionID: request.SessionID,
	}
	if request.FinishedAt != nil {
		result.FinishedAt = timestamp(*request.FinishedAt)
	}
	return result
}

func sessionResponse(session app.Session) SessionResponse {
	result := SessionResponse{
		ID: session.ID, ConnectionID: session.ConnectionID, Wallet: WalletResponse{Kind: string(session.Wallet.Kind), Name: session.Wallet.Name, RDNS: session.Wallet.RDNS},
		Account: session.Account, ChainID: session.ChainID, State: "active", CreatedAt: timestamp(session.CreatedAt), LastAccessAt: timestamp(session.LastAccessAt),
	}
	if session.ClosedAt != nil {
		result.State, result.ClosedAt = "closed", timestamp(*session.ClosedAt)
	}
	return result
}
