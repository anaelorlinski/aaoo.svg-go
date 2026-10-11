//go:build race

package svg

// the race detector slows the code down 5 to 10 times
const timeScale = 10
