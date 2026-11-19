// Package api exposes the local service HTTP interface.
//
// @title Wallet Bridge Local API
// @version 1.0
// @description Local service access. Wallet and browser authorization APIs are delivered in later iterations.
// @host 127.0.0.1:18765
// @BasePath /api/v1
// @schemes http
// @securityDefinitions.apikey ClientBearer
// @in header
// @name Authorization
// @description Bearer token from the current user's client.key.
// @securityDefinitions.apikey ControlBearer
// @in header
// @name Authorization
// @description Bearer token from the current user's control.key; required for administrative operations.
package api
