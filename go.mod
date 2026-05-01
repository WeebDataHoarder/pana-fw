module git.gammaspectra.live/WeebDataHoarder/pana-fw

go 1.26

require github.com/4kills/go-zlib v1.2.0

require (
	git.gammaspectra.live/WeebDataHoarder/go-inthex v0.0.0-20260219194340-56b84313b3ff
	github.com/rasky/go-lzo v0.0.0-20200203143853-96a758eda86e
)

require (
	github.com/ably/vcdiff-go v0.0.2
	github.com/bluekeyes/go-gitdiff v0.8.1
	github.com/goccy/go-yaml v1.19.2
	github.com/icholy/digest v1.1.0
	github.com/klauspost/compress v1.18.6
	golang.org/x/sync v0.20.0
)

require (
	git.gammaspectra.live/WeebDataHoarder/go-zopfli v0.0.0-20260522010654-f1864dc03768
	github.com/ulikunitz/xz v0.5.15
	golang.org/x/image v0.41.0
)

replace github.com/ably/vcdiff-go v0.0.2 => git.gammaspectra.live/WeebDataHoarder/vcdiff-go v0.0.0-20260502054726-48a7585a8b73
