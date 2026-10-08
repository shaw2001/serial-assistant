package web

import "embed"

//go:embed index.html app.css app.js OPPOSans-Regular.woff2 bridge.js THIRD_PARTY_NOTICES.txt
var Assets embed.FS
