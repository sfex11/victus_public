package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ArXivClient provides access to ArXiv for research-based strategy generation
// Inspired by Quant-Autoresearch's ArXiv RAG system
type ArXivClient struct {
	client    *http.Client
	baseURL   string
	cache     map[string][]Paper
	cacheTTL  time.Duration
	lastFetch map[string]time.Time
}

// Paper represents an ArXiv paper
type Paper struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Abstract    string   `json:"abstract"`
	Authors     []string `json:"authors"`
	Published   string   `json:"published"`
	Updated     string   `json:"updated"`
	Categories  []string `json:"categories"`
	DOI         string   `json:"doi"`
	PDFURL      string   `json:"pdf_url"`
	ArXivURL    string   `json:"arxiv_url"`
	RelevanceScore float64 `json:"relevance_score"`
}

// ResearchContext provides research context for strategy generation
type ResearchContext struct {
	Query   string
	Papers  []Paper
	Summary string
}

// NewArXivClient creates a new ArXiv client
func NewArXivClient() *ArXivClient {
	return &ArXivClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:   "http://export.arxiv.org/api/query",
		cache:     make(map[string][]Paper),
		cacheTTL:  24 * time.Hour,
		lastFetch: make(map[string]time.Time),
	}
}

// Search searches ArXiv for papers matching the query
func (c *ArXivClient) Search(ctx context.Context, query string, maxResults int) ([]Paper, error) {
	// Check cache
	if papers, ok := c.cache[query]; ok {
		if time.Since(c.lastFetch[query]) < c.cacheTTL {
			return papers, nil
		}
	}

	// Build URL
	params := url.Values{}
	params.Set("search_query", fmt.Sprintf("all:%s", query))
	params.Set("start", "0")
	params.Set("max_results", fmt.Sprintf("%d", maxResults))
	params.Set("sortBy", "relevance")
	params.Set("sortOrder", "descending")

	reqURL := fmt.Sprintf("%s?%s", c.baseURL, params.Encode())

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from ArXiv: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ArXiv returned status %d", resp.StatusCode)
	}

	// Parse XML response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	papers, err := parseArXivXML(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ArXiv response: %w", err)
	}

	// Update cache
	c.cache[query] = papers
	c.lastFetch[query] = time.Now()

	return papers, nil
}

// SearchQuantFinance searches for quantitative finance papers
func (c *ArXivClient) SearchQuantFinance(ctx context.Context, topic string, maxResults int) ([]Paper, error) {
	// Add q-fin category and topic
	query := fmt.Sprintf("cat:q-fin AND all:%s", topic)
	return c.Search(ctx, query, maxResults)
}

// GetResearchContext gets research context for a hypothesis
func (c *ArXivClient) GetResearchContext(ctx context.Context, hypothesis string, maxPapers int) (*ResearchContext, error) {
	// Extract keywords from hypothesis
	keywords := extractKeywords(hypothesis)
	
	var allPapers []Paper
	for _, keyword := range keywords {
		papers, err := c.SearchQuantFinance(ctx, keyword, maxPapers/len(keywords)+1)
		if err != nil {
			continue // Continue with other keywords
		}
		allPapers = append(allPapers, papers...)
	}

	// Deduplicate
	allPapers = deduplicatePapers(allPapers)

	// Sort by relevance
	sortPapersByRelevance(allPapers, hypothesis)

	// Limit
	if len(allPapers) > maxPapers {
		allPapers = allPapers[:maxPapers]
	}

	// Generate summary
	summary := generateResearchSummary(allPapers)

	return &ResearchContext{
		Query:   hypothesis,
		Papers:  allPapers,
		Summary: summary,
	}, nil
}

// BuildPromptWithResearch builds a strategy generation prompt with research context
func (c *ArXivClient) BuildPromptWithResearch(ctx context.Context, hypothesis, symbol string, maxPapers int) (string, error) {
	research, err := c.GetResearchContext(ctx, hypothesis, maxPapers)
	if err != nil {
		// Return basic prompt without research
		return buildBasicPrompt(hypothesis, symbol), nil
	}

	prompt := fmt.Sprintf(`
You are a quantitative trading strategy designer. Generate a trading strategy based on the following research.

HYPOTHESIS: %s
SYMBOL: %s

RESEARCH CONTEXT:
%s

RELEVANT PAPERS:
`, hypothesis, symbol, research.Summary)

	for i, paper := range research.Papers {
		prompt += fmt.Sprintf(`
%d. %s
   Authors: %s
   Abstract: %s
   URL: %s
`, i+1, paper.Title, strings.Join(paper.Authors, ", "), 
			truncate(paper.Abstract, 300), paper.ArXivURL)
	}

	prompt += `
Based on the above research, generate a trading strategy in YAML format.

Requirements:
1. Use technical indicators: EMA, SMA, RSI, MACD, ATR
2. Use SINGLE comparison per entry/exit
3. Stop-loss: 0.01 to 0.05 (1-5%)
4. Position size: 50-200 USDT
5. Max positions: 1-2

Output YAML format:
name: "StrategyName"
symbol: "` + symbol + `"
type: "hedge"
long:
  entry: "price < ema(20)"
  exit: "price > ema(20)"
  stop_loss: 0.02
short:
  entry: "price > ema(20)"
  exit: "price < ema(20)"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2
`

	return prompt, nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// parseArXivXML parses ArXiv XML response
func parseArXivXML(data []byte) ([]Paper, error) {
	var papers []Paper

	// Simple XML parsing (in production, use encoding/xml)
	// This is a simplified version
	entries := strings.Split(string(data), "<entry>")
	
	for i, entry := range entries {
		if i == 0 {
			continue // Skip header
		}

		paper := Paper{
			ID:         extractXML(entry, "id"),
			Title:      cleanText(extractXML(entry, "title")),
			Abstract:   cleanText(extractXML(entry, "summary")),
			Published:  extractXML(entry, "published"),
			Updated:    extractXML(entry, "updated"),
			ArXivURL:   extractXML(entry, "id"),
		}

		// Extract authors
		authors := strings.Split(entry, "<author>")
		for j, auth := range authors {
			if j == 0 {
				continue
			}
			name := extractXML(auth, "name")
			if name != "" {
				paper.Authors = append(paper.Authors, name)
			}
		}

		// Extract categories
		cats := strings.Split(entry, "category term=\"")
		for j, cat := range cats {
			if j == 0 {
				continue
			}
			cat = strings.Split(cat, "\"")[0]
			if cat != "" {
				paper.Categories = append(paper.Categories, cat)
			}
		}

		// Build PDF URL
		if paper.ID != "" {
			paper.PDFURL = strings.Replace(paper.ID, "abs", "pdf", 1) + ".pdf"
		}

		papers = append(papers, paper)
	}

	return papers, nil
}

// extractXML extracts content between XML tags
func extractXML(data, tag string) string {
	start := strings.Index(data, "<"+tag+">")
	if start == -1 {
		return ""
	}
	start += len(tag) + 2

	end := strings.Index(data[start:], "</"+tag+">")
	if end == -1 {
		return ""
	}

	return data[start : start+end]
}

// cleanText cleans text content
func cleanText(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "  ", " ")
	return strings.TrimSpace(text)
}

// truncate truncates text to max length
func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

// extractKeywords extracts keywords from hypothesis
func extractKeywords(hypothesis string) []string {
	// Remove common words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"will": true, "be": true, "to": true, "for": true, "and": true,
		"or": true, "in": true, "on": true, "at": true, "of": true,
		"that": true, "this": true, "it": true, "with": true,
	}

	words := strings.Fields(strings.ToLower(hypothesis))
	var keywords []string

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'")
		if len(word) > 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	// Add trading-specific keywords
	tradingKeywords := []string{"trading", "strategy", "momentum", "volatility", "trend"}
	for _, kw := range tradingKeywords {
		if strings.Contains(strings.ToLower(hypothesis), kw) {
			keywords = append(keywords, kw)
		}
	}

	if len(keywords) == 0 {
		keywords = []string{"quantitative trading"}
	}

	return keywords
}

// deduplicatePapers removes duplicate papers
func deduplicatePapers(papers []Paper) []Paper {
	seen := make(map[string]bool)
	var result []Paper

	for _, paper := range papers {
		if !seen[paper.ID] {
			seen[paper.ID] = true
			result = append(result, paper)
		}
	}

	return result
}

// sortPapersByRelevance sorts papers by relevance to hypothesis
func sortPapersByRelevance(papers []Paper, hypothesis string) {
	// Simple relevance scoring based on keyword overlap
	keywords := extractKeywords(hypothesis)
	
	for i := range papers {
		score := 0.0
		paperText := strings.ToLower(papers[i].Title + " " + papers[i].Abstract)
		
		for _, kw := range keywords {
			if strings.Contains(paperText, strings.ToLower(kw)) {
				score += 1.0
			}
		}
		
		papers[i].RelevanceScore = score
	}

	// Sort by score (descending)
	for i := 0; i < len(papers); i++ {
		for j := i + 1; j < len(papers); j++ {
			if papers[j].RelevanceScore > papers[i].RelevanceScore {
				papers[i], papers[j] = papers[j], papers[i]
			}
		}
	}
}

// generateResearchSummary generates a summary of research papers
func generateResearchSummary(papers []Paper) string {
	if len(papers) == 0 {
		return "No relevant research found."
	}

	summary := fmt.Sprintf("Found %d relevant papers:\n\n", len(papers))

	for i, paper := range papers {
		if i >= 3 {
			break // Only summarize top 3
		}
		summary += fmt.Sprintf("- %s: %s\n\n", 
			paper.Title, 
			truncate(paper.Abstract, 200))
	}

	return summary
}

// buildBasicPrompt builds a basic prompt without research
func buildBasicPrompt(hypothesis, symbol string) string {
	return fmt.Sprintf(`
You are a quantitative trading strategy designer. Generate a trading strategy.

HYPOTHESIS: %s
SYMBOL: %s

Requirements:
1. Use technical indicators: EMA, SMA, RSI
2. Use SINGLE comparison per entry/exit
3. Stop-loss: 0.01 to 0.05
4. Position size: 50-200 USDT

Output YAML format with name, symbol, type, long, short, risk sections.
`, hypothesis, symbol)
}

// MarshalJSON implements JSON marshaling for Paper
func (p Paper) MarshalJSON() ([]byte, error) {
	type Alias Paper
	return json.Marshal(&struct {
		Alias
	}{
		Alias: (Alias)(p),
	})
}
