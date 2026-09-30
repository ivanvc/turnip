# Never built. This file only pins the golangci-lint version `make lint`
# downloads, in a form Dependabot's docker ecosystem tracks: a Dependabot
# pull request bumping this tag moves CI and local runs to the new
# release. The Makefile reads the tag and downloads the matching release
# binary (verified against the release's checksums), as golangci-lint's
# maintainers recommend over building it from source; the image is not
# used.
FROM docker.io/golangci/golangci-lint:v2.14.0
