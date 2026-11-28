package volumeservice

import (
	"fmt"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/text/prosemirror"
)

// MaxDocumentBytes is the largest document file a save or CreateFile
// stores (forge 35a-1); a larger one is refused with document_too_large.
const MaxDocumentBytes = 5 << 20

// IsText reports whether a file type is text the index, retrieval and
// kb.read take: markdown as it is, a document through its derived
// Markdown (TextOf).
func IsText(fileType string) bool {
	return fileType == "markdown" || fileType == "document"
}

// TextOf is the Markdown of a file's content: a markdown file's own text,
// a document's derived Markdown (prosemirror.Markdown), whose lines are
// what citations of a document name.
func TextOf(fileType string, content []byte) (string, error) {
	switch fileType {
	case "markdown":
		return string(content), nil
	case "document":
		doc, err := prosemirror.Parse(content)
		if err != nil {
			return "", err
		}
		return prosemirror.Markdown(doc), nil
	}
	return "", fmt.Errorf("volumeservice: %q files have no text", fileType)
}

// DocumentFile checks content a writer or CreateFile stores as a document:
// a document file, kept byte for byte, or a bare ProseMirror doc, wrapped
// as one (prosemirror.Envelope). Empty content is an empty document.
func DocumentFile(args features.ServiceArgs, content []byte) ([]byte, *features.ServiceResponse) {
	if len(content) == 0 {
		return prosemirror.Empty(), nil
	}
	if len(content) > MaxDocumentBytes {
		return nil, args.Fail(http.StatusRequestEntityTooLarge, Log.Code("document_too_large").Errorf("the document is %d KB, more than the %d KB allowed", len(content)>>10, MaxDocumentBytes>>10))
	}
	file, err := prosemirror.Envelope(content)
	if err != nil {
		return nil, args.Fail(http.StatusBadRequest, Log.Code("invalid_document").Wrap(err))
	}
	return file, nil
}
