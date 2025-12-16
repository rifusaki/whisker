module github.com/rifusaki/whisker

go 1.25.5

require (
	github.com/joho/godotenv v1.5.1
	gopkg.in/telebot.v3 v3.3.8
)

require github.com/ggerganov/whisper.cpp/bindings/go v0.0.0-20251213073744-2551e4ce98db

require (
	github.com/go-audio/audio v1.0.0 // indirect
	github.com/go-audio/riff v1.0.0 // indirect
	github.com/go-audio/wav v1.1.0 // indirect
)

replace github.com/ggerganov/whisper.cpp/bindings/go => /home/rifubuntu/whisper.cpp/bindings/go
