module github.com/anaelorlinski/aaoo.svg-go/svgtext

go 1.26.0

require (
	github.com/anaelorlinski/aaoo.cssengine-go v0.1.1-0.20261011145628-6a6191e8f372
	github.com/anaelorlinski/aaoo.gfxcore-go v0.0.0
	github.com/anaelorlinski/aaoo.svg-go v0.0.0
	github.com/anaelorlinski/aaoo.textengine-go v0.0.0
)

require (
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/go-text/typesetting v0.3.6-0.20261006155729-e49e2bdf1e4b // indirect
	golang.org/x/image v0.23.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/anaelorlinski/aaoo.svg-go => ..

replace github.com/anaelorlinski/aaoo.cssengine-go => ../../aaoo.cssengine-go

replace github.com/anaelorlinski/aaoo.gfxcore-go => ../../aaoo.gfxcore-go

replace github.com/anaelorlinski/aaoo.textengine-go => ../../aaoo.textengine-go
