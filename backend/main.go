package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"log"
	"path/filepath"
	"strings"
	"errors"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	pdf "github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
)

type AnalyzeRequest struct {
	ResumeText string `json:"resume_text"`
	JobDesc    string `json:"job_desc"`
}

type ValidationResponse struct {
	IsResume         bool   `json:"is_resume"`
	IsJobDescription bool   `json:"is_job_description"`
	Message          string `json:"message"`
}

var aiCaller = callOpenAI

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(cors.Default())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.POST("/analyze", func(c *gin.Context) {
		var req AnalyzeRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			log.Println("Invalid analyze request body:", err)
			c.JSON(400, gin.H{"error": "Invalid request"})
			return
		}

		if len(strings.TrimSpace(req.ResumeText)) < 100 {
			log.Println("Resume validation failed: resume too short")
			c.JSON(400, gin.H{"error": "Resume content looks too short. Please upload or paste a valid resume."})
			return
		}

		if len(strings.TrimSpace(req.JobDesc)) < 100 {
			log.Println("Job description validation failed: job description too short")
			c.JSON(400, gin.H{"error": "Job description looks too short. Please paste a valid job posting."})
			return
		}

		validationPrompt := `
			You are validating inputs for a resume analyzer.

			Decide if the first text is a real candidate resume.
			A valid resume usually contains multiple resume-like sections such as:
			- Professional Summary
			- Skills
			- Work Experience
			- Projects
			- Education
			- Certifications
			- Achievements

			Reject documents that are not resumes, including:
			- Training plans
			- Immigration/STEM OPT documents
			- Offer letters
			- Job descriptions
			- Forms
			- Letters
			- Certificates
			- Random notes

			Decide if the second text is a real job description or job requirement.
			A valid job description usually includes role title, required skills, qualifications, responsibilities, location, or experience requirements.

			Return ONLY valid JSON in this exact format:
			{
			"is_resume": true,
			"is_job_description": true,
			"message": "Valid inputs"
			}

			If invalid, explain clearly in message.

			Resume Text:
			` + req.ResumeText + `

			Job Description:
			` + req.JobDesc

		validationText, err := aiCaller(validationPrompt)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		var validation ValidationResponse
		if err := json.Unmarshal([]byte(cleanJSON(validationText)), &validation); err != nil {
			log.Println("Could not parse validation response:", err)
			c.JSON(500, gin.H{"error": "Could not validate resume/job description"})
			return
		}

		if !validation.IsResume || !validation.IsJobDescription {
			log.Println("Validation failed:", validation.Message)
			c.JSON(400, gin.H{"error": validation.Message})
			return
		}

		prompt := `
			Analyze this resume against the job description.

			If the resume is valid but not strongly targeted to the job description, clearly explain the mismatch in the Resume Match Summary and Top Resume Improvements.

			Return the response EXACTLY in this format:

			ATS Score: <score>/100

			Resume Match Summary:
			<short paragraph>

			Top Resume Improvements:
			1. <improvement>
			2. <improvement>
			3. <improvement>
			4. <improvement>
			5. <improvement>

			Interview Questions:
			1. <question>
			2. <question>
			3. <question>
			4. <question>
			5. <question>

			LinkedIn Recruiter Message:
			<short message>

			Resume:
			` + req.ResumeText + `

			Job Description:
			` + req.JobDesc

		output, err := aiCaller(prompt)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		log.Println("Analysis completed successfully")
		c.JSON(200, gin.H{"result": output})
	})

	r.POST("/upload-resume", func(c *gin.Context) {
		file, err := c.FormFile("resume")
		if err != nil {
			log.Println("Resume upload failed: missing file")
			c.JSON(400, gin.H{"error": "Resume file is required"})
			return
		}

		log.Println("Resume file upload received:", file.Filename)

		ext := strings.ToLower(filepath.Ext(file.Filename))

		tempFile, err := os.CreateTemp("", "resume-*"+ext)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not create temp file"})
			return
		}
		defer os.Remove(tempFile.Name())
		defer tempFile.Close()

		src, err := file.Open()
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not open uploaded file"})
			return
		}
		defer src.Close()

		if _, err := io.Copy(tempFile, src); err != nil {
			c.JSON(500, gin.H{"error": "Could not save uploaded file"})
			return
		}

		var text string

		switch ext {
		case ".pdf":
			text, err = extractPDFText(tempFile.Name(), file.Size)
		case ".docx":
			text, err = extractDOCXText(tempFile.Name())
		case ".txt":
			textBytes, readErr := os.ReadFile(tempFile.Name())
			if readErr != nil {
				err = readErr
			} else {
				text = string(textBytes)
			}
		default:
			log.Println("Unsupported file type:", ext)
			c.JSON(400, gin.H{"error": "Unsupported file type. Please upload PDF, DOCX, or TXT."})
			return
		}

		if err != nil {
			log.Println("Could not read uploaded file content:", err)
			c.JSON(500, gin.H{"error": "Could not read file content"})
			return
		}

		log.Println("Resume file processed successfully:", file.Filename)
		c.JSON(200, gin.H{"text": text})
	})

	return r
}

func main() {
	r := setupRouter()
	r.Run(":8080")
}

func extractPDFText(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	reader, err := pdf.NewReader(f, size)
	if err != nil {
		return "", err
	}

	var text string
	totalPage := reader.NumPage()

	for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
		page := reader.Page(pageIndex)
		if page.V.IsNull() {
			continue
		}

		pageText, err := page.GetPlainText(nil)
		if err == nil {
			text += pageText + "\n"
		}
	}

	return text, nil
}

func extractDOCXText(path string) (string, error) {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	doc := r.Editable()
	return doc.GetContent(), nil
}

func callOpenAI(prompt string) (string, error) {
	body := map[string]interface{}{
		"model": "gpt-4.1-mini",
		"input": prompt,
	}

	jsonBody, _ := json.Marshal(body)

	httpReq, _ := http.NewRequest("POST", "https://api.openai.com/v1/responses", bytes.NewBuffer(jsonBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Println("OpenAI request failed:", err)
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Println("OpenAI API error:", string(data))
		return "", errors.New(strings.NewReplacer("\n", " ").Replace(string(data)))
	}

	var openAIResponse map[string]interface{}

	if err := json.Unmarshal(data, &openAIResponse); err != nil {
		return "", err
	}

	output := ""

	if responseOutput, ok := openAIResponse["output"].([]interface{}); ok {
		for _, item := range responseOutput {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			contentArr, ok := itemMap["content"].([]interface{})
			if !ok {
				continue
			}

			for _, content := range contentArr {
				contentMap, ok := content.(map[string]interface{})
				if !ok {
					continue
				}

				if text, ok := contentMap["text"].(string); ok {
					output += text + "\n"
				}
			}
		}
	}

	if output == "" {
		return "", errors.New("OpenAI returned no text")
	}

	log.Println("OpenAI response processed successfully")
	return strings.TrimSpace(output), nil
}

func cleanJSON(input string) string {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "```json")
	input = strings.TrimPrefix(input, "```")
	input = strings.TrimSuffix(input, "```")
	return strings.TrimSpace(input)
}