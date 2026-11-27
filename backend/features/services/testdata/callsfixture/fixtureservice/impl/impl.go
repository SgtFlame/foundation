package impl

// The fixture calls the config service through its proxy without
// declaring it (manifest_test.go).
import _ "github.com/SgtFlame/foundation/backend/features/config/services/configservice/proxy"
