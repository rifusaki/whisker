package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/queue"
	"github.com/rifusaki/whisker/internal/timings"
	tele "gopkg.in/telebot.v3"
)

// whisperLangs is the set of language codes accepted by whisper.cpp.
// Both ISO 639-1 codes and English names are valid inputs.
// Source: https://github.com/ggerganov/whisper.cpp/blob/master/src/whisper.cpp
var whisperLangs = map[string]string{
	// ISO code → canonical code (mostly identity, a few aliases)
	"af": "af", "am": "am", "ar": "ar", "as": "as", "az": "az",
	"ba": "ba", "be": "be", "bg": "bg", "bn": "bn", "bo": "bo",
	"br": "br", "bs": "bs", "ca": "ca", "cs": "cs", "cy": "cy",
	"da": "da", "de": "de", "el": "el", "en": "en", "es": "es",
	"et": "et", "eu": "eu", "fa": "fa", "fi": "fi", "fo": "fo",
	"fr": "fr", "gl": "gl", "gu": "gu", "ha": "ha", "haw": "haw",
	"he": "he", "hi": "hi", "hr": "hr", "ht": "ht", "hu": "hu",
	"hy": "hy", "id": "id", "is": "is", "it": "it", "ja": "ja",
	"jw": "jw", "ka": "ka", "kk": "kk", "km": "km", "kn": "kn",
	"ko": "ko", "la": "la", "lb": "lb", "ln": "ln", "lo": "lo",
	"lt": "lt", "lv": "lv", "mg": "mg", "mi": "mi", "mk": "mk",
	"ml": "ml", "mn": "mn", "mr": "mr", "ms": "ms", "mt": "mt",
	"my": "my", "ne": "ne", "nl": "nl", "nn": "nn", "no": "no",
	"oc": "oc", "pa": "pa", "pl": "pl", "ps": "ps", "pt": "pt",
	"ro": "ro", "ru": "ru", "sa": "sa", "sd": "sd", "si": "si",
	"sk": "sk", "sl": "sl", "sn": "sn", "so": "so", "sq": "sq",
	"sr": "sr", "su": "su", "sv": "sv", "sw": "sw", "ta": "ta",
	"te": "te", "tg": "tg", "th": "th", "tk": "tk", "tl": "tl",
	"tr": "tr", "tt": "tt", "uk": "uk", "ur": "ur", "uz": "uz",
	"vi": "vi", "yi": "yi", "yo": "yo", "yue": "yue", "zh": "zh",
	// English full names → ISO code
	"afrikaans": "af", "amharic": "am", "arabic": "ar", "assamese": "as",
	"azerbaijani": "az", "bashkir": "ba", "belarusian": "be", "bulgarian": "bg",
	"bengali": "bn", "tibetan": "bo", "breton": "br", "bosnian": "bs",
	"catalan": "ca", "czech": "cs", "welsh": "cy", "danish": "da",
	"german": "de", "greek": "el", "english": "en", "spanish": "es",
	"estonian": "et", "basque": "eu", "persian": "fa", "finnish": "fi",
	"faroese": "fo", "french": "fr", "galician": "gl", "gujarati": "gu",
	"hausa": "ha", "hawaiian": "haw", "hebrew": "he", "hindi": "hi",
	"croatian": "hr", "haitian creole": "ht", "hungarian": "hu",
	"armenian": "hy", "indonesian": "id", "icelandic": "is", "italian": "it",
	"japanese": "ja", "javanese": "jw", "georgian": "ka", "kazakh": "kk",
	"khmer": "km", "kannada": "kn", "korean": "ko", "latin": "la",
	"luxembourgish": "lb", "lingala": "ln", "lao": "lo", "lithuanian": "lt",
	"latvian": "lv", "malagasy": "mg", "maori": "mi", "macedonian": "mk",
	"malayalam": "ml", "mongolian": "mn", "marathi": "mr", "malay": "ms",
	"maltese": "mt", "myanmar": "my", "nepali": "ne", "dutch": "nl",
	"norwegian nynorsk": "nn", "norwegian": "no", "occitan": "oc",
	"punjabi": "pa", "polish": "pl", "pashto": "ps", "portuguese": "pt",
	"romanian": "ro", "russian": "ru", "sanskrit": "sa", "sindhi": "sd",
	"sinhala": "si", "slovak": "sk", "slovenian": "sl", "shona": "sn",
	"somali": "so", "albanian": "sq", "serbian": "sr", "sundanese": "su",
	"swedish": "sv", "swahili": "sw", "tamil": "ta", "telugu": "te",
	"tajik": "tg", "thai": "th", "turkmen": "tk", "tagalog": "tl",
	"turkish": "tr", "tatar": "tt", "ukrainian": "uk", "urdu": "ur",
	"uzbek": "uz", "vietnamese": "vi", "yiddish": "yi", "yoruba": "yo",
	"cantonese": "yue", "chinese": "zh",
}

// defaultLang is used when no per-chat language has been set.
const defaultLang = "auto"

// Handler holds the dependencies for the bot.
type Handler struct {
	Bot    *tele.Bot
	client *audio.Client
	queue  *queue.Queue
	// langMap stores the language preference per chat ID (int64 → string).
	langMap sync.Map
}

// NewHandler initializes the bot and registers routes.
func NewHandler(token string, client *audio.Client, q *queue.Queue) (*Handler, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		Bot:    b,
		client: client,
		queue:  q,
	}

	b.Handle(tele.OnVoice, h.handleVoice)
	b.Handle(tele.OnAudio, h.handleAudio)
	b.Handle(tele.OnText, h.handleText)

	return h, nil
}

func (h *Handler) Start() {
	fmt.Println("Starting bot...")
	h.Bot.Start()
}

// handleText intercepts plain text messages to check for language commands.
// Any message that is exactly a valid whisper language code or name (case-
// insensitive) sets that chat's language preference.
// "auto" clears the preference (back to automatic detection).
func (h *Handler) handleText(c tele.Context) error {
	input := strings.TrimSpace(strings.ToLower(c.Text()))

	if input == "auto" {
		h.langMap.Delete(c.Chat().ID)
		return c.Send("Language reset to auto-detect.")
	}

	if code, ok := whisperLangs[input]; ok {
		h.langMap.Store(c.Chat().ID, code)
		return c.Send(fmt.Sprintf("Language set to: %s\nNext audio will be transcribed in that language.", code))
	}

	// Not a language command — ignore (don't reply to every message).
	return nil
}

// langFor returns the language set for a chat, falling back to defaultLang.
func (h *Handler) langFor(chatID int64) string {
	if v, ok := h.langMap.Load(chatID); ok {
		return v.(string)
	}
	return defaultLang
}

func (h *Handler) handleVoice(c tele.Context) error {
	voice := c.Message().Voice
	if voice == nil {
		return c.Send("No voice file found in the message.")
	}
	h.Transcriber(c, &voice.File)
	return nil
}

func (h *Handler) handleAudio(c tele.Context) error {
	audio := c.Message().Audio
	if audio == nil {
		return c.Send("No audio file found in the message.")
	}
	h.Transcriber(c, &audio.File)
	return nil
}

// Transcriber handles the full lifecycle of a transcription request:
//  1. Download the Telegram file to a temp path
//  2. Submit the job to the queue (notifying the user of their position)
//  3. Block until the queue worker delivers the transcript
//  4. Reply with the result or an error message
func (h *Handler) Transcriber(c tele.Context, file *tele.File) error {
	msg := c.Message()
	msgID := 0
	chatID := int64(0)
	if msg != nil {
		msgID = msg.ID
		if msg.Chat != nil {
			chatID = msg.Chat.ID
		}
	}

	lang := h.langFor(chatID)

	start := time.Now()
	step := start
	logPrefix := fmt.Sprintf("[telegram chat=%d msg=%d lang=%s]", chatID, msgID, lang)
	timings.Printf("%s transcription start", logPrefix)

	// Create a temp directory for the downloaded audio.
	tmpDir, err := os.MkdirTemp("", "whisker-*")
	if err != nil {
		return c.Send("Internal error creating temp dir.")
	}
	defer os.RemoveAll(tmpDir)
	timings.Printf("%s tempdir created in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Download the audio file from Telegram.
	srcPath := filepath.Join(tmpDir, "input_audio")
	if err := h.Bot.Download(file, srcPath); err != nil {
		return c.Send("Failed to download file.")
	}
	if timings.DetailedEnabled() {
		if info, err := os.Stat(srcPath); err == nil {
			timings.Detailedf("%s download stats (path=%s size=%d bytes)", logPrefix, srcPath, info.Size())
		}
	}
	timings.Printf("%s download finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Submit to the queue. The returned position tells us how many jobs are
	// ahead of this one so we can give an accurate status reply.
	job := &queue.Job{
		AudioPath: srcPath,
		Language:  lang,
		Result:    make(chan queue.JobResult, 1),
	}
	pos := h.queue.Submit(job)
	timings.Printf("%s queued (position=%d) in %s", logPrefix, pos, time.Since(step).Truncate(time.Millisecond))

	// Notify the user now — before blocking on the result — so they know the
	// bot received their message even if they have to wait.
	c.Send(queue.PositionMessage(pos)) //nolint:errcheck — best-effort notice

	// Block until the worker finishes this job.
	result := <-job.Result
	timings.Printf("%s transcription finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	if result.Err != nil {
		return c.Send("Transcription failed: " + result.Err.Error())
	}

	text := result.Text
	if timings.DetailedEnabled() {
		preview := text
		if len(preview) > 200 {
			preview = preview[:200]
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		preview = strings.ReplaceAll(preview, "\r", " ")
		preview = strings.Join(strings.Fields(preview), " ")
		if preview == "" {
			preview = "<empty>"
		}
		timings.Detailedf("%s transcript preview=%q (len=%d)", logPrefix, preview, len(text))
	}

	if text == "" {
		timings.Printf("%s total time %s (no speech)", logPrefix, time.Since(start).Truncate(time.Millisecond))
		return c.Send("[No speech detected]")
	}

	err = c.Send(fmt.Sprintf("Transcript:\n\n%s", text))
	timings.Printf("%s reply sent in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	timings.Printf("%s total time %s", logPrefix, time.Since(start).Truncate(time.Millisecond))
	return err
}
