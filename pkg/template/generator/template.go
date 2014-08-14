package generator

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type Template struct {
	HttpClient *http.Client
	expresion  string
}

const (
	Alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Numerals = "0123456789"
	Ascii    = Alphabet + Numerals + "~!@#$%^&*()-_+={}[]\\|<,>.?/\"';:`"
)

var (
	rangeExp      = regexp.MustCompile(`([\\]?[a-zA-Z0-9]\-?[a-zA-Z0-9]?)`)
	generatorsExp = regexp.MustCompile(`\[([a-zA-Z0-9\-\\]+)\](\{([0-9]+)\})`)
	remoteExp     = regexp.MustCompile(`\[GET\:(http(s)?:\/\/(.+))\]`)
)

type GeneratorExprRanges [][]byte

func (t Template) randomInt(n int) int {
	return rand.Intn(n)
}

func (t Template) alphabetSlice(from, to byte) (string, error) {
	leftPos := strings.Index(Ascii, string(from))
	rightPos := strings.LastIndex(Ascii, string(to))
	if leftPos > rightPos {
		return "", fmt.Errorf("Invalid range specified: %s-%s", string(from), string(to))
	}
	return Ascii[leftPos:rightPos], nil
}

func (t Template) replaceWithGenerated(s *string, expresion string, ranges [][]byte, length int) error {
	var alphabet string
	for _, r := range ranges {
		switch string(r[0]) + string(r[1]) {
		case `\w`:
			alphabet += Ascii
		case `\d`:
			alphabet += Numerals
		case `\a`:
			alphabet += Alphabet + Numerals
		default:
			if slice, err := t.alphabetSlice(r[0], r[1]); err != nil {
				return err
			} else {
				alphabet += slice
			}
		}
	}
	if len(alphabet) == 0 {
		return fmt.Errorf("Empty range in expresion: %s", expresion)
	}
	result := make([]byte, length, length)
	for i := 0; i <= length-1; i++ {
		result[i] = alphabet[t.randomInt(len(alphabet))]
	}
	*s = strings.Replace(*s, expresion, string(result), 1)
	return nil
}

func (t Template) findExpresionPos(s string) GeneratorExprRanges {
	matches := rangeExp.FindAllStringIndex(s, -1)
	result := make(GeneratorExprRanges, len(matches), len(matches))
	for i, r := range matches {
		result[i] = []byte{s[r[0]], s[r[1]-1]}
	}
	return result
}

func (t Template) rangesAndLength(s string) (string, int, error) {
	l := strings.LastIndex(s, "{")
	// If the length ({}) is not specified in expresion,
	// then assume the length is 1 character
	//
	if l > 0 {
		expr := s[0:strings.LastIndex(s, "{")]
		length, err := t.parseLength(s)
		return expr, length, err
	} else {
		return s, 1, nil
	}
}

func (t Template) parseLength(s string) (int, error) {
	lengthStr := string(s[strings.LastIndex(s, "{")+1 : len(s)-1])
	if l, err := strconv.Atoi(lengthStr); err != nil {
		return 0, fmt.Errorf("Unable to parse length from %v", s)
	} else {
		return l, nil
	}
}

func (t Template) httpGet(url string) (string, error) {
	if t.HttpClient == nil {
		t.HttpClient = &http.Client{}
	}
	resp, err := t.HttpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	return string(body), err
}

func (t Template) replaceUrlWithData(s *string, expresion string) error {
	result, err := t.httpGet(expresion[5 : len(expresion)-1])
	if err != nil {
		return err
	}
	*s = strings.Replace(*s, expresion, strings.TrimSpace(result), 1)
	return nil
}

func (t Template) Process() (string, error) {
	result := t.expresion
	genMatches := generatorsExp.FindAllStringIndex(t.expresion, -1)
	remMatches := remoteExp.FindAllStringIndex(t.expresion, -1)
	// Parse [a-z]{} types
	for _, r := range genMatches {
		ranges, length, err := t.rangesAndLength(t.expresion[r[0]:r[1]])
		if err != nil {
			return "", err
		}
		positions := t.findExpresionPos(ranges)
		if err := t.replaceWithGenerated(&result, t.expresion[r[0]:r[1]], positions, length); err != nil {
			return "", err
		}
	}
	// Parse [GET:<url>] type
	//
	for _, r := range remMatches {
		if err := t.replaceUrlWithData(&result, t.expresion[r[0]:r[1]]); err != nil {
			return "", err
		}
	}
	return result, nil
}
