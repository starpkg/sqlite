module github.com/starpkg/sqlite

go 1.18

require (
	github.com/1set/starlet v0.1.3
	github.com/starpkg/base v0.0.4
	go.starlark.net v0.0.0-20240123142251-f86470692795
	modernc.org/sqlite v1.26.0
)

replace github.com/starpkg/base => ../base
