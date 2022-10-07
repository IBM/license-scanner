// SPDX-License-Identifier: Apache-2.0

package normalizer

import (
	"crypto/md5" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mrutkows/sbom-utility/log"
	"golang.org/x/exp/slices"
)

const (
	NoteTagPattern          = `(?i)<<note[:=].+?>>`
	WildcardMatchingPattern = `(?i)<<match=\.\+>>`

	OptionalWildcardMatchingPattern = `<<match=\.\*>>`

	ReplaceableTextPattern     = `(?i)<<(?:var;(?:name=(.+?);)?(?:original=(.*?);)?)?match=(.+?)>>`
	BeginOptionalLinePattern   = `(?im)^<<beginOptional(?:;name=.*?)?>>`
	BeginOptionalPattern       = `(?i)<<beginOptional(?:;name=.*?)?>>`
	OmitableLine               = "<<omitable>>\n"
	Omitable                   = "<<omitable>>"
	EndOptionalPattern         = `(?i)<<endOptional>>`
	ReplaceEndPattern          = `<</omitable>>`
	CommentBlockOutsidePattern = `(?m)^\s*(?:/\*|-{2,3}\[=*\[)|(?:\*/|]=*])\s*$`
	CommentBlockInsidePattern  = `(?m)^\s*[*#]|\*$`
	CommentLinePattern         = `(?m)^\s*(?://|>|--|;{1,4})`
	HtmlStyleCommentPattern    = `(?m)^\s*<!--|-->\s*$`
	DashLikePattern            = "[\u002D\u2010\u2011\u2013\u2014\u2015\u2212\uFE58\uFE63\uFE0D]"
	QuoteLikePattern           = "[\u0022\u0027\u0060\u00B4\u2018\u2019\u201C\u201D]+"
	HTTPPattern                = `(?i)https?`
	BulletsAndNumberingPattern = "(?m)^\\s*(?:[*+\u2022-]|\\(?(?:\\w|\\*|[\\divx#]+)[.)])\\s+(\\w?)"
	SplitWords                 = `(?m)\b-$\s+\b`
	HorizontalRulePattern      = `(?m)^\s*[*=-]{3,}`
	Copyright                  = `©|\([cC]\)`
	ControlCharacters          = "[\u0000-\u0007\u000E-\u001B]"
	OddCharactersPattern       = "(?im)^\\^l$|\u0080|\u0099|\u009C|\u009D|\u00AC|\u00E2|\u00A7|\u00C2|\u00A4|\u0153|\u20AC|\uFFFD"
	LeadingWhitespacePattern   = `^\s`
	MiddleWhitespacePattern    = "(?:\\s|\u00B7)+"
	TrailingWhitespacePattern  = `\s$`
)

var (
	Logger         = log.NewLogger(log.INFO)
	replacementREs = initVarietalWordSpellings()

	NoteTagPatternRE                  = regexp.MustCompile(NoteTagPattern)
	WildcardMatchingPatternRE         = regexp.MustCompile(WildcardMatchingPattern)
	OptionalWildcardMatchingPatternRE = regexp.MustCompile(OptionalWildcardMatchingPattern)
	BeginOptionalLinePatternRE        = regexp.MustCompile(BeginOptionalLinePattern)
	BeginOptionalPatternRE            = regexp.MustCompile(BeginOptionalPattern)
	EndOptionalPatternRE              = regexp.MustCompile(EndOptionalPattern)
	HorizontalRulePatternRE           = regexp.MustCompile(HorizontalRulePattern)
	SplitWordsRE                      = regexp.MustCompile(SplitWords)
	HTTPPatternRE                     = regexp.MustCompile(HTTPPattern)
	QuoteLikeRE                       = regexp.MustCompile(QuoteLikePattern)
	DashLikeRE                        = regexp.MustCompile(DashLikePattern)
	ReplaceableTextPatternRE          = regexp.MustCompile(ReplaceableTextPattern)
	BulletsAndNumberingPatternRE      = regexp.MustCompile(BulletsAndNumberingPattern)
	CommentBlockOutsideRE             = regexp.MustCompile(CommentBlockOutsidePattern)
	CommentBlockInsideRE              = regexp.MustCompile(CommentBlockInsidePattern)
	HtmlStyleCommentRE                = regexp.MustCompile(HtmlStyleCommentPattern)
	CommentLineRE                     = regexp.MustCompile(CommentLinePattern)
	MiddleWhitespaceRE                = regexp.MustCompile(MiddleWhitespacePattern)
	LeadingWhitespaceRE               = regexp.MustCompile(LeadingWhitespacePattern)
	TrailingWhitespaceRE              = regexp.MustCompile(TrailingWhitespacePattern)
	OddCharactersPatternRE            = regexp.MustCompile(OddCharactersPattern)
	CopyrightRE                       = regexp.MustCompile(Copyright)
	ControlCharactersRE               = regexp.MustCompile(ControlCharacters)
)

//go:embed replacement_words.json
var replacementWordsBytes []byte

func initVarietalWordSpellings() map[string]*regexp.Regexp {
	REMap := make(map[string]*regexp.Regexp)

	// Unmarshal replacement_words.json, compile REs, save in map (just once)
	replacementWords := make(map[string]string)
	if err := json.Unmarshal(replacementWordsBytes, &replacementWords); err != nil {
		panic(err)
	} else {
		for replacement, pattern := range replacementWords {
			re, err := regexp.Compile(pattern)
			if err != nil {
				panic(err)
			}
			REMap[replacement] = re
		}
	}

	return REMap
}

// NormalizationData holds the input data and its normalized text
type NormalizationData struct {
	// original input text
	OriginalText string
	// normalized version of the input text
	NormalizedText string
	IndexMap       []int
	CaptureGroups  []*CaptureGroup
	Hash           Digest
}

type CaptureGroup struct {
	GroupNumber int
	Name        string
	Original    string
	Matches     string
}

// Digest provides an option to store a combination of hashes of a given package
type Digest struct {
	// Md5
	Md5 string
	// sha256
	Sha256 string
	// sha512
	Sha512 string
}

// NormalizeText normalizes the input text
func (n *NormalizationData) NormalizeText() error {
	// verify that the original text is a string with a length of at least one.
	if len(n.OriginalText) < 1 {
		Logger.Error("Invalid text")
		return fmt.Errorf("failed to normalize data: invalid input text with length %d", len(n.OriginalText))
	}

	// Check if the text contains control characters indicative of binary or non-text files.
	// match against /[\u0000-\u0007\u000E-\u001B]/
	if ControlCharactersRE.MatchString(n.OriginalText) {
		return fmt.Errorf("failed to normalize data: invalid input text with control characters")
	}

	// TODO: remove excessive whitespace, prior to generating the index map.
	// TODO: keep track of the removed sections while stripping off excessive whitespace
	// TODO: TBD ^^^ Why bother when we will replace all whitespace with single " " later on?

	// remove note tags
	n.removeNoteTags()

	// replace the wild card matching pattern <<match=.+>> with the range of the permitted number of characters i.e. 1, 144
	n.limitWildcardMatching()

	// replace optional wild card matching pattern <<match=.*>> with the range of the permitted number of characters i.e. 0, 144
	n.limitOptionalWildcardMatching()

	// Capture replaceable text sections. (Guideline 2.1.3)
	n.captureReplaceableTextSections()

	// Replace the optional tags with <<omitable>> and <</omitable>>. (Guideline 2.1.4)
	n.standardizeOmitableTags()

	// Convert the input text to all lower case. (Guideline 4.1.1)
	n.NormalizedText = strings.ToLower(n.NormalizedText)

	// remove odd characters, such as TM, replacement character ?, etc
	// NOTE! Remove these before any use of regexp2 because rune chars throw off the index map
	n.removeOddCharacters()

	// Remove code comment indicators. (Guideline 6.1.1)
	n.removeCodeCommentIndicators()

	// SPDX matching guideline 5.1.2 (Hyphens, Dashes)
	// Any hyphen, dash, en dash, em dash, or other variations should be considered equivalent
	n.replaceDashLikeCharacters()

	// SPDX matching guideline 5.1.3 (Quotes)
	// Any variation of quotations (single, double, curly, etc.) should be considered equivalent
	n.replaceQuoteLikeCharacters()

	// SPDX matching guideline 13.1.1 - Standardize to http
	// To avoid a license mismatch due to a difference in a hyperlink protocol (e.g. http vs. https).
	// HTTP:// and HTTPS:// should be considered equivalent.
	// Templates may or may not include markup for this guideline.
	n.standardizeToHTTP()

	// SPDX matching guideline 7.1.1 (Bullets and Numbering)
	// To avoid the possibility of a non-match due to the otherwise same license using bullets instead of numbers,
	// number instead of letter, or no bullets instead of bullet, etc., for a list of clauses.
	// The guideline says to ignore the list item for matching purposes but we do not ignore
	// TODO: should we continue with this or we should change this and start ignoring the list item
	n.removeBulletsAndNumbering()

	// reconnect split words
	n.reconnectSplitWords()

	// remove horizontal rules
	n.removeHorizontalRules()

	// Replace varietal word spelling. (Guideline 8.1.1)
	n.replaceVarietalWordSpellings()

	// SPDX matching guideline 9.1.1 (Copyright Symbol)
	// By having a rule regarding the use of “©”, “(c)”, or “copyright”,
	// we avoid the possibility of a mismatch based on these variations.
	// “©”, “(c)”, or “Copyright” should be considered equivalent and interchangeable.
	// Templates do not include markup for this guideline so we replace all of these with `copyright`
	n.replaceCopyrightSymbols()

	// Remove HTML tags
	n.removeHTMLTags()

	// TODO: Decode HTML entities.
	// n.decodeHTMLEntities()

	// Replace all whitespace with a single space. (Guideline 3.1.1)
	// To avoid the possibility of a non-match due to different spacing of words, line breaks, or paragraphs.
	// All whitespace should be treated as a single blank space.
	n.replaceWhitespace()

	// Add Hash Digest
	// calculate MD5 for the normalized text
	md5hash := md5.Sum([]byte(n.NormalizedText)) //nolint:gosec
	n.Hash.Md5 = hex.EncodeToString(md5hash[:])

	// calculate SHA256 for the normalized text
	sha2hash := sha256.Sum256([]byte(n.NormalizedText))
	n.Hash.Sha256 = hex.EncodeToString(sha2hash[:])

	// calculate SHA512 for the normalized text
	sha5hash := sha512.Sum512([]byte(n.NormalizedText))
	n.Hash.Sha512 = hex.EncodeToString(sha5hash[:])

	return nil
}

// initializeIndexMap initializes the index map based on the normalized text
func (n *NormalizationData) initialize() {
	// initialize the normalized text with the original text
	if len(n.NormalizedText) == 0 {
		n.NormalizedText = n.OriginalText
	}

	// generate an index map, to map the normalized text indices back to the respective index in the original text
	if len(n.IndexMap) == 0 {
		l := len(n.NormalizedText)
		n.IndexMap = make([]int, l)
		for i := 0; i < l; i++ {
			n.IndexMap[i] = i
		}
	}
}

func (n *NormalizationData) removeNoteTags() {
	n.regexpReplacePatternAndUpdateIndexMap(NoteTagPatternRE, " ")
}

// limitWildcardMatching replaces the wild card matching pattern <<match=.+>> with the permitted number of characters i.e. 1, 144
func (n *NormalizationData) limitWildcardMatching() {
	n.regexpReplacePatternAndUpdateIndexMap(WildcardMatchingPatternRE, `<<.{1,144}>>`)
}

// limitOptionalWildcardMatching replaces the wild card matching pattern <<match=.*>> with the permitted number of characters i.e. 0, 144
func (n *NormalizationData) limitOptionalWildcardMatching() {
	n.regexpReplacePatternAndUpdateIndexMap(OptionalWildcardMatchingPatternRE, `<<.{0,144}>>`)
}

// regexpRemovePatternAndUpdateIndexMap removes all occurrences matching the regex from in the normalized text
// the index map is updated based on the updated normalized text
// Array of strings returned is the set of unique strings from submatches, if any
func (n *NormalizationData) regexpRemovePatternAndUpdateIndexMap(re *regexp.Regexp) []string {
	return n.regexpReplacePatternAndUpdateIndexMap(re, "")
}

// regexpReplacePatternAndUpdateIndexMap replaces all occurrences matching the regexp2 pattern from in the normalized text
// the normalized text has this pattern match replaced with the replacement string
// the index map is updated based on the updated normalized text
func (n *NormalizationData) regexpReplacePatternAndUpdateIndexMap(re *regexp.Regexp, replacement string) []string {
	n.initialize() // initialize normalized text and index map if not set already
	allSubmatchIndex := re.FindAllStringSubmatchIndex(n.NormalizedText, len(n.NormalizedText))
	return n.replaceMatchesWithStringAndUpdateIndexMap(allSubmatchIndex, replacement)
}

// captureReplaceableTextSections implements Guidelines 2.1.3 from https://spdx.dev/license-list/matching-guidelines/
// captures and replaces replaceable text sections in form of capture groups
func (n *NormalizationData) captureReplaceableTextSections() {
	n.initialize() // initialize normalized text and index map if not set already
	allSubmatchIndex := ReplaceableTextPatternRE.FindAllStringSubmatchIndex(n.NormalizedText, len(n.NormalizedText))

	// Need to build replacement string list based on the submatches (to use the common text/indexMap update)
	var replacements []string
	for _, match := range allSubmatchIndex {
		var submatches []string
		for i := 3; i < len(match); i = i + 2 { // 0:2 is the whole match.  Following pairs (2:4, 4:6, 6:8) are submatches
			firstIndex := match[i-1]
			lastIndex := match[i]
			if firstIndex < 0 || lastIndex < 0 { // Not found submatches are -1 -1
				submatches = append(submatches, "") // Use empty string for these
			} else {
				submatches = append(submatches, n.NormalizedText[match[i-1]:match[i]])
			}
		}
		name := submatches[0]
		original := submatches[1]
		regex := submatches[2]

		// If the match="regex" is dquoted, trim the dquotes. SPDX templates look like match="regex",
		// but only do it when both start and end are found because legacy was unquoted and sometimes
		// just happened to start with an optional dqoute like <<match="?match this maybe quoted"?>>
		// Notice when we start with this optional dquote we expect seeing and end with an optional dquote+questionmark
		// so checking for suffix AND prefix works for this backwards compatibility
		if strings.HasPrefix(regex, `"`) && strings.HasSuffix(regex, `"`) {
			regex = regex[1 : len(regex)-1]
		}

		// If the regex ends in an unprotected greedy quantifier, make it lazy.
		if strings.HasSuffix(regex, "+") || strings.HasSuffix(regex, "*") || strings.HasSuffix(regex, "}") {
			regex += "?"
		}

		// Special case match=".{0,5000}" is often seen in SPDX templates. We want to treat it like the wildcard case
		// and limit it.
		// Someday this should probably be smarter about detecting range >= n, but only the 0,5000 check is needed now.
		if strings.HasSuffix(regex, "{0,5000}?") {
			regex = regex[0:len(regex)-len("{0,5000}?")] + "{0,1000}?"
		}

		replacementText := "<<" + regex + ">>"
		replacements = append(replacements, replacementText)

		// Save the capture group data
		c := &CaptureGroup{
			GroupNumber: len(n.CaptureGroups) + 1,
			Name:        name,
			Original:    original,
			Matches:     regex,
		}
		n.CaptureGroups = append(n.CaptureGroups, c)
	}

	n.replaceMatchesWithStringsAndUpdateIndexMap(allSubmatchIndex, replacements)
}

func (n *NormalizationData) standardizeOmitableTags() {
	n.regexpReplacePatternAndUpdateIndexMap(BeginOptionalLinePatternRE, OmitableLine) // Allows other $(m)^ matches
	n.regexpReplacePatternAndUpdateIndexMap(BeginOptionalPatternRE, Omitable)
	n.regexpReplacePatternAndUpdateIndexMap(EndOptionalPatternRE, ReplaceEndPattern)
}

func (n *NormalizationData) removeCodeCommentIndicators() {
	// Remove comment block indicators
	n.regexpReplacePatternAndUpdateIndexMap(CommentBlockOutsideRE, " ")
	n.regexpReplacePatternAndUpdateIndexMap(CommentBlockInsideRE, " ")

	// Remove HTML-style comments
	// the HTML comments are replaced first before matching single line comment to avoid accidental partial matching of
	// the HTML comment tags with the CommentLinePattern expression i.e. -- | >
	n.regexpReplacePatternAndUpdateIndexMap(HtmlStyleCommentRE, " ")

	// Remove comment line indicators
	n.regexpReplacePatternAndUpdateIndexMap(CommentLineRE, " ")
}

func (n *NormalizationData) removeHTMLTags() {
	// This is a code-around (and optimization) for negative lookahead in HTMLTagPattern = `<(?!http)[^<>]+>(?!>)`
	s := "<"

	textLen := len(n.NormalizedText)
	var allSubmatchIndex [][]int
	next := 0
	for i := strings.Index(n.NormalizedText, s); i > -1; i = strings.Index(n.NormalizedText[next:], s) {
		i = next + i // position in the full normalized text string
		next = i + 1 // if we continue to loop, start one char after the last hit -- also used for lookahead position

		if textLen > next+len("http") && n.NormalizedText[next:next+len("http")] == "http" {
			continue // negative lookahead.  Ignoring <http... links.
		}

		// move past contents until forbidden char or end char
		j := i
		if textLen > j+1 {
			j++
			for ; textLen > j && n.NormalizedText[j] != '<' && n.NormalizedText[j] != '>'; j++ {
			}
		}

		// lookahead again to ignore >>
		if textLen > j && n.NormalizedText[j] == '>' && (textLen <= j+1 || n.NormalizedText[j+1] != '>') {
			next = j + 1 // if we continue to loop, start one char after the last hit -- also end match index
			allSubmatchIndex = append(allSubmatchIndex, []int{i, next})
		}
	}

	// remove the matches
	n.replaceMatchesWithStringAndUpdateIndexMap(allSubmatchIndex, "")
}

func (n *NormalizationData) replaceDashLikeCharacters() {
	n.regexpReplacePatternAndUpdateIndexMap(DashLikeRE, "-")
}

func (n *NormalizationData) replaceQuoteLikeCharacters() {
	n.regexpReplacePatternAndUpdateIndexMap(QuoteLikeRE, "'")
}

func (n *NormalizationData) standardizeToHTTP() {
	n.regexpReplacePatternAndUpdateIndexMap(HTTPPatternRE, "http")
}

func (n *NormalizationData) removeBulletsAndNumbering() {
	n.initialize() // initialize normalized text and index map if not set already
	allSubmatchIndex := BulletsAndNumberingPatternRE.FindAllStringSubmatchIndex(n.NormalizedText, len(n.NormalizedText))

	// Need to build replacement string list based on the submatches (to use the common text/indexMap update)
	var replacements []string
	for _, match := range allSubmatchIndex {
		firstIndex := match[2]
		lastIndex := match[3]
		if firstIndex < 0 || lastIndex < 0 { // Not found submatches are -1 -1
			replacements = append(replacements, "") // Use empty string for these
		} else {
			replacements = append(replacements, n.NormalizedText[firstIndex:lastIndex])
		}
	}

	n.replaceMatchesWithStringsAndUpdateIndexMap(allSubmatchIndex, replacements)
}

func (n *NormalizationData) reconnectSplitWords() {
	n.regexpRemovePatternAndUpdateIndexMap(SplitWordsRE)
}

func (n *NormalizationData) removeHorizontalRules() {
	n.regexpReplacePatternAndUpdateIndexMap(HorizontalRulePatternRE, " ")
}

// replaceVarietalWordSpellings will read replacement words JSON file and replace matches
// to create a consistent representation of words with alternate spellings.
// The replacement may be a word or regexp pattern.
// The file is only read once.
// The patterns are only compiled once.
// The compiled regexp are kept in a map for reuse.
func (n *NormalizationData) replaceVarietalWordSpellings() {
	for replacement, re := range replacementREs {
		n.regexpReplacePatternAndUpdateIndexMap(re, replacement)
	}
}

func (n *NormalizationData) replaceCopyrightSymbols() {
	n.regexpReplacePatternAndUpdateIndexMap(CopyrightRE, "copyright")
}

func (n *NormalizationData) removeOddCharacters() {
	n.regexpReplacePatternAndUpdateIndexMap(OddCharactersPatternRE, " ")
}

func (n *NormalizationData) replaceWhitespace() {
	n.regexpReplacePatternAndUpdateIndexMap(MiddleWhitespaceRE, " ")
	n.regexpRemovePatternAndUpdateIndexMap(LeadingWhitespaceRE)
	n.regexpRemovePatternAndUpdateIndexMap(TrailingWhitespaceRE)
}

func (n *NormalizationData) replaceMatchesWithStringAndUpdateIndexMap(allSubmatchIndex [][]int, replacement string) []string {
	// Here we want the same replacement string to be used for all the matches. So build the right-sized array with repeated strings.
	allMatches := len(allSubmatchIndex)
	if allMatches == 0 {
		return nil
	}
	replacements := make([]string, allMatches)
	for i := 0; i < allMatches; i++ {
		replacements[i] = replacement
	}

	var submatches []string
	// extract submatch strings, if any, to return to the caller
	for _, match := range allSubmatchIndex {
		for i := 3; i < len(match); i += 2 {
			from := match[i-1]
			to := match[i]
			if from >= 0 && to > from { // skipping -1 or invalid range
				submatch := substr(n.NormalizedText, from, to)
				if !slices.Contains(submatches, submatch) {
					// only keep the unique strings
					submatches = append(submatches, submatch)
				}
			}
		}
	}

	n.replaceMatchesWithStringsAndUpdateIndexMap(allSubmatchIndex, replacements)
	return submatches
}

// substr will do text[from:to] or text[from:] depending on to >= len(text)
func substr(text string, from int, to int) string {
	if to >= len(text) {
		return text[from:]
	}
	return text[from:to]
}

// replaceMatchesWithStringAndUpdateIndexMap iterates over matches to:
// * remove or replace the matched text
// * build an updated index map
// * retrieve and return unique submatch strings, if applicable
func (n *NormalizationData) replaceMatchesWithStringsAndUpdateIndexMap(allSubmatchIndex [][]int, replacements []string) {
	if allSubmatchIndex == nil {
		return
	}

	var newText string
	var newIndex []int

	prev := 0
	for i, match := range allSubmatchIndex {

		firstIndex := match[0]
		lastIndex := match[1]
		replacement := replacements[i]

		// copy the text and index map before (and in between) matches
		if firstIndex > prev {
			newText += n.NormalizedText[prev:firstIndex]
			newIndex = append(newIndex, n.IndexMap[prev:firstIndex]...)
		}

		if len(replacement) > 0 {
			// If a replacement string is being inserted, then we'll also insert indexes as follows:
			// * The first element should be the first index in the replaced section.
			// * The last element should be the last index in the replaced section. (Unless there is only a single char)
			// * Middle elements should be -1, for 'replaced'. A match starting/ending on these indices is invalid.
			replacementIndex := make([]int, len(replacement))
			for c := range replacement {
				switch c {
				case 0:
					replacementIndex[c] = n.IndexMap[firstIndex]
				case len(replacement) - 1:
					replacementIndex[c] = n.IndexMap[lastIndex-1]
				default:
					replacementIndex[c] = -1
				}
			}

			// Append the replacement text and indexes
			newText += replacement
			newIndex = append(newIndex, replacementIndex...)
		}

		prev = lastIndex
	}

	// Append the remaining text and indexes, if there are more after the last match
	if prev < len(n.IndexMap) {
		newText += n.NormalizedText[prev:]
		newIndex = append(newIndex, n.IndexMap[prev:]...)
	}

	// Set the new text and index map
	n.NormalizedText = newText
	n.IndexMap = newIndex
}
