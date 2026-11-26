package recordhandlers

import (
	"io"
	"mime"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// OpenFile handles GET /api/v1/records/files/:entity/:record/:field/:file,
// the URL a record's file is served at (files.URL), so a browser can
// link, show or download it.
func (rh *RecordHandler) OpenFile(c *gin.Context) {
	args := rest.Args(c)

	resp := rh.service.OpenFile(recordservice.OpenFileArgs{ServiceArgs: args, Entity: c.Param("entity"), Record: c.Param("record"), Field: c.Param("field"), File: c.Param("file")})
	if resp.Common.Code != 200 {
		c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
		return
	}
	defer resp.Content.Close()
	contentType := resp.File.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Uploaded content is served from the app's own origin, so only types
	// a browser cannot run show inline; HTML, SVG and the rest download,
	// sandboxed
	disposition := "attachment"
	if inline(contentType) {
		disposition = "inline"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": resp.File.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
	c.DataFromReader(200, resp.File.Size, contentType, io.Reader(resp.Content), nil)
}

// inline reports whether a content type is safe to show in the browser.
func inline(contentType string) bool {
	t, _, _ := mime.ParseMediaType(contentType)
	switch {
	case t == "image/svg+xml":
		return false
	case strings.HasPrefix(t, "image/"), strings.HasPrefix(t, "audio/"), strings.HasPrefix(t, "video/"):
		return true
	}
	return t == "application/pdf" || t == "text/plain"
}
