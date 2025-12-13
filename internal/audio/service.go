package audio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Service handles audio conversion and transcription interactions
type Service struct {
	ModelName string
}

// create a new instance of the transcription service
func NewService(modelName string) *Service {
	return &Service{ModelName: modelName}
}

// convert to WAV using ffmpeg
func (s *Service) ConvertToWav(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", inputPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", outputPath)
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}
	return nil
}

// Transcribe calls the whisper CLI wrapper. 
func (s *Service) Transcribe(audioPath string) (string, error) {
	// execute the whisper command line tool.
	cmd := exec.Command("whisper", audioPath, "--model", s.ModelName, "--output_format", "txt", "--output_dir", filepath.Dir(audioPath))
	
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper execution failed: %w", err)
	}

	// whisper CLI writes to a file, so we need to read that file back
	txtPath := strings.ReplaceAll(audioPath, "wav", "txt")
	content, err := os.ReadFile(txtPath)
	if err != nil {
		return "", fmt.Errorf("failed to read transcript file: %w", err)
	}

	return strings.TrimSpace(string(content)), nil
}