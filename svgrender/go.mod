module github.com/anaelorlinski/aaoo.svg-go/svgrender

go 1.26.0

require (
	github.com/anaelorlinski/aaoo.cssengine-go v0.1.1-0.20261011145628-6a6191e8f372
	github.com/anaelorlinski/aaoo.drawengine-go v0.0.0
	github.com/anaelorlinski/aaoo.gfxcore-go v0.0.0
	github.com/anaelorlinski/aaoo.svg-go v0.0.0
	github.com/anaelorlinski/aaoo.svg-go/svgtext v0.0.0
	github.com/anaelorlinski/aaoo.textengine-go v0.0.0
	github.com/go-text/typesetting v0.3.6-0.20261006155729-e49e2bdf1e4b
	golang.org/x/net v0.61.0
)

require (
	github.com/BurntSushi/freetype-go v0.0.0-20160129220410-b763ddbfe298 // indirect
	github.com/BurntSushi/graphics-go v0.0.0-20160129215708-b43f31a4a966 // indirect
	github.com/BurntSushi/xgb v0.0.0-20210121224620-deaf085860bc // indirect
	github.com/BurntSushi/xgbutil v0.0.0-20190907113008-ad855c713046 // indirect
	github.com/anaelorlinski/clipper2 v0.0.0-00010101000000-000000000000 // indirect
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/srwiley/scanx v0.0.0-20190309010443-e94503791388 // indirect
	github.com/tdewolff/minify/v2 v2.24.19 // indirect
	github.com/tdewolff/parse/v2 v2.8.16 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/anaelorlinski/aaoo.svg-go => ..

replace github.com/anaelorlinski/aaoo.svg-go/svgtext => ../svgtext

replace github.com/anaelorlinski/aaoo.cssengine-go => ../../aaoo.cssengine-go

replace github.com/anaelorlinski/aaoo.gfxcore-go => ../../aaoo.gfxcore-go

replace github.com/anaelorlinski/aaoo.drawengine-go => ../../aaoo.drawengine-go

replace github.com/anaelorlinski/aaoo.textengine-go => ../../aaoo.textengine-go

replace github.com/anaelorlinski/otf2ttf-go => ../../otf2ttf-go

replace github.com/anaelorlinski/clipper2 => ../../clipper2
