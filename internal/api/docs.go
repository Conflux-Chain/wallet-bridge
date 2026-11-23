// Package api exposes the local service HTTP interface.
//
// @title Wallet Bridge Local API
// @version 1.3
// @description Local service access, browser authorization and client connection/Session lifecycle. Browser wallet execution endpoints are delivered separately.
// @host 127.0.0.1:18765
// @BasePath /api/v1
// @schemes http
// @securityDefinitions.apikey ServiceBearer
// @in header
// @name Authorization
// @description Bearer token from the current user's client.key, shared by trusted local clients for service access and management.
// @securityDefinitions.apikey BrowserCookie
// @in header
// @name Cookie
// @description Swagger 2.0 represents this cookie as a header. Use wallet_bridge_browser=<secret>; only browser status and logout accept it. Browser clients receive it through Set-Cookie, never through JSON.
package api
