package identityhandlers

import (
	"net/http"
	"time"

	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/gin-gonic/gin"
)

// setSession puts a session's tokens in their cookies: HTTP-only,
// SameSite=Strict, and Secure when the request came over HTTPS. A session
// without a new refresh token keeps the browser's.
func setSession(c *gin.Context, s identity.Session) {
	setCookie(c, identity.AccessCookie, s.Access, identity.AccessPath, s.AccessExpires)
	if s.Refresh != "" {
		setCookie(c, identity.RefreshCookie, s.Refresh, identity.RefreshPath, s.RefreshExpires)
	}
}

// clearSession removes both cookies.
func clearSession(c *gin.Context) {
	setCookie(c, identity.AccessCookie, "", identity.AccessPath, time.Unix(0, 0))
	setCookie(c, identity.RefreshCookie, "", identity.RefreshPath, time.Unix(0, 0))
}

func setCookie(c *gin.Context, name, value, path string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: name, Value: value, Path: path, Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
	})
}

// refreshToken is the request's refresh cookie, or "".
func refreshToken(c *gin.Context) string {
	cookie, err := c.Request.Cookie(identity.RefreshCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}
