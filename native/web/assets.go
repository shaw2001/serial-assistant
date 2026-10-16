package web

import "embed"

//go:embed index.html app.css app.js OPPOSans-Regular.woff2 wallpaper.jpg bridge.js THIRD_PARTY_NOTICES.txt
var Assets embed.FS
