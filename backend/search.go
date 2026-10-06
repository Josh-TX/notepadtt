package backend

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxSearchResults   = 10
	maxSectionsPerFile = 3
	snippetLines       = 4
	searchTimeout      = 10 * time.Second
	filenameMatchBonus = 1000
)

type SearchSection struct {
	Snippet         string `json:"snippet"`
	StartLineNumber int    `json:"startLineNumber"`
}

type SearchResult struct {
	Path     string          `json:"path"`
	Sections []SearchSection `json:"sections"`
}

type searchHit struct {
	path  string
	lines []int // matching line numbers (capped per file by grep -m)
	score int
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	useRegex := r.URL.Query().Get("regex") == "true"
	if q == "" {
		writeJSON(w, []SearchResult{})
		return
	}
	var terms []string
	if useRegex {
		if _, err := regexp.Compile("(?i)" + q); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		terms = []string{q}
	} else {
		terms = strings.Fields(q)
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	hits := map[string]*searchHit{}
	if err := s.contentSearch(ctx, terms, useRegex, hits); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.filenameSearch(terms, useRegex, hits)

	ordered := make([]*searchHit, 0, len(hits))
	for _, h := range hits {
		ordered = append(ordered, h)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		return ordered[i].path < ordered[j].path
	})
	if len(ordered) > maxSearchResults {
		ordered = ordered[:maxSearchResults]
	}

	results := make([]SearchResult, 0, len(ordered))
	for _, h := range ordered {
		results = append(results, SearchResult{Path: h.path, Sections: s.buildSections(h)})
	}
	writeJSON(w, results)
}

// contentSearch shells out to ripgrep (preferred) or grep and records per-file
// matching line numbers in hits. Exit status 1 just means "no matches".
func (s *Server) contentSearch(ctx context.Context, terms []string, useRegex bool, hits map[string]*searchHit) error {
	var cmd *exec.Cmd
	if rg, err := exec.LookPath("rg"); err == nil {
		args := []string{"--null", "--line-number", "--no-heading", "--with-filename", "--ignore-case",
			"--hidden", "--no-ignore", "--follow", "--max-count", strconv.Itoa(maxSectionsPerFile)}
		for name := range excludedNames {
			args = append(args, "--glob", "!"+name)
		}
		if !useRegex {
			args = append(args, "--fixed-strings")
		}
		for _, t := range terms {
			args = append(args, "-e", t)
		}
		args = append(args, "--", ".")
		cmd = exec.CommandContext(ctx, rg, args...)
	} else if grep, err := exec.LookPath("grep"); err == nil {
		args := []string{"-R", "-I", "-n", "-H", "-Z", "-i", "-m", strconv.Itoa(maxSectionsPerFile)}
		for name := range excludedNames {
			args = append(args, "--exclude-dir="+name)
		}
		if useRegex {
			args = append(args, "-E")
		} else {
			args = append(args, "-F")
		}
		for _, t := range terms {
			args = append(args, "-e", t)
		}
		args = append(args, "--", ".")
		cmd = exec.CommandContext(ctx, grep, args...)
	} else {
		return errors.New("neither rg nor grep found in PATH")
	}
	cmd.Dir = s.rootAbs
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) && ctx.Err() == nil {
		// exit 2 can mean "some files unreadable" while still producing matches
		if out.Len() == 0 {
			return err
		}
	}

	for _, line := range strings.Split(out.String(), "\n") {
		nul := strings.IndexByte(line, 0)
		if nul < 0 {
			continue
		}
		rel := strings.TrimPrefix(filepath.ToSlash(line[:nul]), "./")
		rest := line[nul+1:]
		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			continue
		}
		n, err := strconv.Atoi(rest[:colon])
		if err != nil {
			continue
		}
		h := hits[rel]
		if h == nil {
			h = &searchHit{path: rel}
			hits[rel] = h
		}
		h.lines = append(h.lines, n)
		h.score++
	}
	return nil
}

// filenameSearch adds files whose path matches any term.
func (s *Server) filenameSearch(terms []string, useRegex bool, hits map[string]*searchHit) {
	var re *regexp.Regexp
	lowered := make([]string, len(terms))
	for i, t := range terms {
		lowered[i] = strings.ToLower(t)
	}
	if useRegex {
		re = regexp.MustCompile("(?i)" + terms[0])
	}
	walkFollowSymlinks(s.rootAbs, nil, func(p string) error {
		rel, err := filepath.Rel(s.rootAbs, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		match := false
		if re != nil {
			match = re.MatchString(rel)
		} else {
			l := strings.ToLower(path.Base(rel))
			for _, t := range lowered {
				if strings.Contains(l, t) {
					match = true
					break
				}
			}
		}
		if !match {
			return nil
		}
		h := hits[rel]
		if h == nil {
			h = &searchHit{path: rel}
			hits[rel] = h
		}
		h.score += filenameMatchBonus
		return nil
	})
}

// buildSections reads the file and cuts a snippetLines-line snippet around each
// matching line, skipping matches already covered by the previous snippet.
func (s *Server) buildSections(h *searchHit) []SearchSection {
	if len(h.lines) == 0 {
		return []SearchSection{}
	}
	b, err := os.ReadFile(s.abs(h.path))
	if err != nil {
		return []SearchSection{}
	}
	lines := strings.Split(string(b), "\n")
	sections := []SearchSection{}
	prevEnd := 0
	for _, n := range h.lines {
		if len(sections) >= maxSectionsPerFile {
			break
		}
		if n <= prevEnd {
			continue
		}
		start := max(1, n-1)
		end := min(len(lines), start+snippetLines-1)
		if start > end {
			continue
		}
		sections = append(sections, SearchSection{
			Snippet:         strings.Join(lines[start-1:end], "\n"),
			StartLineNumber: start,
		})
		prevEnd = end
	}
	return sections
}
