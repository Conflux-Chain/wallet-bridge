// Package api exposes the local service HTTP interface.
//
// @title Wallet Bridge Local API
// @version 1.2
// @description Local service access and browser authorization. Wallet APIs are delivered in later iterations.
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
