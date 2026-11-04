// Loader fixture: go 1.21 so broken/ needs a newer language version, and a
// dependency outside the module (dep/, through replace, so no download).
module example.com/loadfix

go 1.21

require example.com/dep v0.0.0

replace example.com/dep => ./dep
