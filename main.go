package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const (
	// Groq base URL — swap to "https://api.openai.com/v1" when upgrading to OpenAI
	groqBaseURL = "https://api.groq.com/openai/v1"

	// Free, fast Groq model. Alternatives: "llama3-8b-8192", "gemma2-9b-it"
	defaultModel = "llama-3.3-70b-versatile"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Portfolio struct {
	Profile                      json.RawMessage `json:"profile"`
	PersonalInfo                 json.RawMessage `json:"personal_info"`
	PersonalityTraits            json.RawMessage `json:"personality_traits"`
	MotivationsAndPrinciples     json.RawMessage `json:"motivations_and_principles"`
	CareerGoals                  json.RawMessage `json:"career_goals"`
	WorkPreferences              json.RawMessage `json:"work_preferences"`
	TechnicalBackground          json.RawMessage `json:"technical_background"`
	ProfessionalExperienceSummary json.RawMessage `json:"professional_experience_summary"`
	CareerStories                json.RawMessage `json:"career_stories"`
	InterviewReadyAnswers        json.RawMessage `json:"interview_ready_answers"`
	InterestsAndPreferences      json.RawMessage `json:"interests_and_preferences"`
	PortfolioAIVoice             json.RawMessage `json:"portfolio_ai_voice"`
	Experience                   json.RawMessage `json:"experience"`
	Projects                     json.RawMessage `json:"projects"`
	CaseStudies                  json.RawMessage `json:"case_studies"`
}

type AskRequest struct {
	Question string `json:"question"`
}

// OpenAI-compatible chat completions request / response
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Globals
// ---------------------------------------------------------------------------

var portfolioContext string

// ---------------------------------------------------------------------------
// Portfolio loader
// ---------------------------------------------------------------------------

func loadPortfolio(path string) error {
	var raw []byte

	// Prefer the env var (used in production / GitHub secrets)
	if env := os.Getenv("PORTFOLIO_JSON"); env != "" {
		log.Println("Portfolio loaded from PORTFOLIO_JSON env var.")
		raw = []byte(env)
	} else {
		// Fall back to local file for development
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read portfolio file: %w", err)
		}
		log.Printf("Portfolio loaded from file: %s", path)
		raw = data
	}

	var p Portfolio
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse portfolio JSON: %w", err)
	}
	compact, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal portfolio: %w", err)
	}
	portfolioContext = string(compact)
	return nil
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

func askHandler(c echo.Context) error {
	var req AskRequest
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "question is required"})
	}

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error": "GROQ_API_KEY is not set. Add it to your .env file.",
		})
	}

	systemPrompt := fmt.Sprintf(`You are an AI assistant embedded in the portfolio website of Bamas Angkasa.
Only answer questions related to his professional background, skills, experience, and projects.
If a question is unrelated, politely redirect the user to portfolio topics.
Keep answers concise and professional.

CRITICAL LANGUAGE RULE: Detect the language of the user's question and reply in that exact language.
- If the question is in English → reply in English
- If the question is in Bahasa Indonesia → reply in Bahasa Indonesia
- Never switch to a different language regardless of what language the knowledge base is written in.

Here is the full portfolio knowledge base:
%s`, portfolioContext)

	payload := chatRequest{
		Model: defaultModel,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: req.Question},
		},
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequest(http.MethodPost, groqBaseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error": "Could not reach Groq API. Check your internet connection.",
		})
	}
	defer resp.Body.Close()

	var result chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to parse Groq response"})
	}

	if result.Error != nil {
		return c.JSON(http.StatusBadGateway, echo.Map{"error": result.Error.Message})
	}

	if len(result.Choices) == 0 {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "empty response from Groq"})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"answer": strings.TrimSpace(result.Choices[0].Message.Content),
	})
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// Load .env file (silently ignored if it doesn't exist)
	_ = godotenv.Load()

	if err := loadPortfolio("data/portfolio.json"); err != nil {
		log.Fatalf("Failed to load portfolio: %v", err)
	}
	log.Println("Portfolio knowledge base loaded.")

	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodPost, http.MethodGet, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderContentType},
	}))
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.POST("/ask", askHandler)
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
	})

	log.Println("Server running on http://localhost:8000")
	e.Logger.Fatal(e.Start(":8000"))
}
