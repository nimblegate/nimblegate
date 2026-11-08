package query

// Negative: the mixed whitespace is inside a raw string literal (SQL text),
// which gofmt leaves alone; every code line is tab-indented.
const topRepos = `
	SELECT repo,
	       count(*) AS pushes
	FROM events
	GROUP BY repo`

func Top() string {
	return topRepos
}
