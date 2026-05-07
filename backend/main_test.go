package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	r := setupRouter()

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCleanJSONPlain(t *testing.T) {
	input := `{"is_resume":true}`
	got := cleanJSON(input)

	if got != `{"is_resume":true}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestCleanJSONWithJSONFence(t *testing.T) {
	input := "```json\n{\"is_resume\":true}\n```"
	got := cleanJSON(input)

	if got != `{"is_resume":true}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestCleanJSONWithCodeFence(t *testing.T) {
	input := "```\n{\"is_resume\":true}\n```"
	got := cleanJSON(input)

	if got != `{"is_resume":true}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestAnalyzeInvalidJSON(t *testing.T) {
	r := setupRouter()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAnalyzeShortResume(t *testing.T) {
	r := setupRouter()

	body := `{
		"resume_text": "short",
		"job_desc": "Firmware Engineer role requiring Embedded C, C++, Linux, FreeRTOS, debugging, system-level troubleshooting, hardware interfaces, and onsite work in Minneapolis."
	}`

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeShortJobDescription(t *testing.T) {
	r := setupRouter()

	body := `{
		"resume_text": "Professional Summary: Embedded C++ engineer with Linux, RTOS, firmware, debugging, unit testing, hardware interfaces, and production software experience across multiple embedded projects.",
		"job_desc": "short"
	}`

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeInvalidResumeValidation(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	aiCaller = func(prompt string) (string, error) {
		return `{
			"is_resume": false,
			"is_job_description": true,
			"message": "This does not look like a resume."
		}`, nil
	}

	r := setupRouter()

	body := `{
		"resume_text": "Training Plan Employee Name Employer Name Training Period STEM OPT document Month 1 Month 2 evaluation training requirements and education plan repeated with formal document structure.",
		"job_desc": "Firmware Engineer role requiring Embedded C, C++, Linux, FreeRTOS, debugging, system-level troubleshooting, hardware interfaces, and onsite work in Minneapolis."
	}`

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeInvalidJobDescriptionValidation(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	aiCaller = func(prompt string) (string, error) {
		return `{
			"is_resume": true,
			"is_job_description": false,
			"message": "This does not look like a job description."
		}`, nil
	}

	r := setupRouter()

	body := `{
		"resume_text": "Professional Summary: Embedded C++ engineer with Linux, RTOS, firmware, debugging, unit testing, hardware interfaces, and production software experience across multiple embedded projects.",
		"job_desc": "Hello this is a random note. Please check this text. It does not contain a real role title, responsibilities, qualifications, job requirements, required skills, location, or experience requirements."
	}`

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeValidationAIFailure(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	aiCaller = func(prompt string) (string, error) {
		return "", errors.New("AI service failed")
	}

	r := setupRouter()

	body := validAnalyzeBody()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeInvalidValidationJSON(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	aiCaller = func(prompt string) (string, error) {
		return "not valid json", nil
	}

	r := setupRouter()

	body := validAnalyzeBody()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeAnalysisAIFailure(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	callCount := 0

	aiCaller = func(prompt string) (string, error) {
		callCount++

		if callCount == 1 {
			return `{
				"is_resume": true,
				"is_job_description": true,
				"message": "Valid inputs"
			}`, nil
		}

		return "", errors.New("AI failed during analysis")
	}

	r := setupRouter()

	body := validAnalyzeBody()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d. body: %s", w.Code, w.Body.String())
	}

	if callCount != 2 {
		t.Fatalf("expected AI to be called twice, got %d", callCount)
	}
}

func TestAnalyzeSuccess(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	callCount := 0

	aiCaller = func(prompt string) (string, error) {
		callCount++

		if callCount == 1 {
			return `{
				"is_resume": true,
				"is_job_description": true,
				"message": "Valid inputs"
			}`, nil
		}

		return `ATS Score: 88/100

Resume Match Summary:
Strong match for embedded C++, Linux, and firmware.

Top Resume Improvements:
1. Add FreeRTOS examples.
2. Add hardware debugging examples.
3. Add metrics.
4. Mention onsite availability.
5. Add embedded C keywords.

Interview Questions:
1. Explain your FreeRTOS experience.
2. Describe embedded Linux debugging.
3. Explain hardware integration.
4. Discuss C++ memory management.
5. Explain firmware testing.

LinkedIn Recruiter Message:
Hi, I am interested in this Firmware Engineer role.`, nil
	}

	r := setupRouter()

	body := validAnalyzeBody()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}

	if !strings.Contains(resp["result"], "ATS Score") {
		t.Fatalf("expected result to contain ATS Score")
	}

	if callCount != 2 {
		t.Fatalf("expected AI to be called twice, got %d", callCount)
	}
}

func TestUploadMissingFile(t *testing.T) {
	r := setupRouter()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.Close()

	req, _ := http.NewRequest("POST", "/upload-resume", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestUploadUnsupportedFile(t *testing.T) {
	r := setupRouter()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, _ := writer.CreateFormFile("resume", "resume.exe")
	part.Write([]byte("fake file"))

	writer.Close()

	req, _ := http.NewRequest("POST", "/upload-resume", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestUploadTxtFile(t *testing.T) {
	r := setupRouter()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, _ := writer.CreateFormFile("resume", "resume.txt")
	part.Write([]byte("Professional Summary: Embedded C++ engineer with Linux, RTOS, firmware, debugging, unit testing, hardware interfaces, and production software experience."))

	writer.Close()

	req, _ := http.NewRequest("POST", "/upload-resume", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}

	if !strings.Contains(resp["text"], "Embedded C++") {
		t.Fatalf("expected extracted text to contain uploaded text")
	}
}

func TestUploadEmptyTxtFile(t *testing.T) {
	r := setupRouter()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, _ := writer.CreateFormFile("resume", "empty.txt")
	part.Write([]byte(""))

	writer.Close()

	req, _ := http.NewRequest("POST", "/upload-resume", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Current backend may return 200 with empty text.
	// Later we should improve backend to return 400.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for current behavior, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestAnalyzeValidationJSONWithFence(t *testing.T) {
	old := aiCaller
	defer func() { aiCaller = old }()

	callCount := 0

	aiCaller = func(prompt string) (string, error) {
		callCount++

		if callCount == 1 {
			return "```json\n{\n  \"is_resume\": true,\n  \"is_job_description\": true,\n  \"message\": \"Valid inputs\"\n}\n```", nil
		}

		return `ATS Score: 90/100

Resume Match Summary:
Good match.

Top Resume Improvements:
1. Add details.
2. Add skills.
3. Add metrics.
4. Add tools.
5. Add project examples.

Interview Questions:
1. Question one?
2. Question two?
3. Question three?
4. Question four?
5. Question five?

LinkedIn Recruiter Message:
Hello recruiter.`, nil
	}

	r := setupRouter()

	body := validAnalyzeBody()

	req, _ := http.NewRequest("POST", "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
}

func validAnalyzeBody() string {
	return `{
		"resume_text": "Professional Summary: Embedded C++ engineer with Linux, RTOS, firmware, debugging, unit testing, hardware interfaces, CAN bus, FreeRTOS, and production software experience across multiple embedded projects.",
		"job_desc": "Firmware Engineer role requiring Embedded C, C++, Linux, FreeRTOS, CANopen, debugging, system-level troubleshooting, hardware interfaces, and onsite work in Minneapolis with 8 plus years of experience."
	}`
}