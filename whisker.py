import os
import tempfile
import subprocess
import logging
import whisper
import dotenv
from telegram import Update
from telegram.ext import ApplicationBuilder, ContextTypes, MessageHandler, filters

# === Configuration ===
dotenv.load_dotenv()

TELEGRAM_TOKEN = # tg_whisper_bot.py
import os
import tempfile
import subprocess
import logging
import whisper
from telegram import Update
from telegram.ext import ApplicationBuilder, ContextTypes, MessageHandler, filters

# === Configuration ===
TELEGRAM_TOKEN = dotenv.get_key(dotenv.find_dotenv(), "TELEGRAM_TOKEN")
MODEL_NAME = "small"   # small/medium/base/large — choose by accuracy/speed/size
# ======================

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Load Whisper model once at startup
print("Loading Whisper model (this can take a bit)...")
model = whisper.load_model(MODEL_NAME)
print("Model loaded.")

async def transcribe_file(audio_path: str) -> str:
    """
    Run whisper transcription on a file path. Returns the transcript text.
    """
    # whisper library expects audio in common container; we convert in handler
    result = model.transcribe(audio_path)
    text = result.get("text", "").strip()
    return text

def convert_to_wav(input_path: str, output_path: str):
    """
    Use ffmpeg to convert input audio to PCM WAV with 16k-48k sample rate that whisper accepts.
    """
    cmd = [
        "ffmpeg",
        "-y",
        "-i", input_path,
        "-ar", "16000",        # sample rate
        "-ac", "1",            # mono
        "-c:a", "pcm_s16le",
        output_path
    ]
    logger.info("Running ffmpeg: %s", " ".join(cmd))
    subprocess.run(cmd, check=True)

async def handle_voice(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Handles voice notes and audio files.
    """
    message = update.effective_message
    sender = message.from_user
    logger.info("Received message from %s: %s", sender.username if sender else "unknown", message.message_id)

    # Choose file: voice (voice note) takes precedence, else audio attachments
    file_obj = None
    if message.voice:
        file_obj = message.voice.get_file()
        filename_hint = "voice.oga"
    elif message.audio:
        file_obj = message.audio.get_file()
        filename_hint = message.audio.file_name or "audio"
    elif message.document and message.document.mime_type and message.document.mime_type.startswith("audio"):
        file_obj = message.document.get_file()
        filename_hint = message.document.file_name or "audio"
    else:
        await message.reply_text("I didn't find an audio attachment in that message. Send a voice note or audio file.")
        return

    # Create temp files
    with tempfile.TemporaryDirectory() as tmpdir:
        incoming_path = os.path.join(tmpdir, filename_hint)
        wav_path = os.path.join(tmpdir, "converted.wav")

        # Download the file
        await file_obj.download_to_drive(incoming_path)
        logger.info("Downloaded file to %s", incoming_path)

        # Convert to WAV suitable for whisper
        try:
            convert_to_wav(incoming_path, wav_path)
        except Exception as e:
            logger.exception("ffmpeg conversion failed: %s", e)
            await message.reply_text("Failed to convert audio file. Make sure ffmpeg is installed.")
            return

        await message.reply_text("Transcribing now... (this might take a moment)")

        # Run whisper transcription
        try:
            transcript = await context.application.run_in_executor(None, lambda: model.transcribe(wav_path).get("text", "").strip())
            if not transcript:
                transcript = "[No speech detected or transcription returned empty text]"
        except Exception as e:
            logger.exception("Whisper failed: %s", e)
            await message.reply_text("Transcription failed. See logs.")
            return

        # Send back the transcript
        # If the transcript is long you could instead write to a text file and send as document.
        max_len_for_message = 4000
        if len(transcript) <= max_len_for_message:
            await message.reply_text(f"Transcript:\n\n{transcript}")
        else:
            # Send as file when it's very long
            txt_path = os.path.join(tmpdir, "transcript.txt")
            with open(txt_path, "w", encoding="utf-8") as f:
                f.write(transcript)
            await message.reply_document(open(txt_path, "rb"), filename="transcript.txt")

async def handle_text(update: Update, context: ContextTypes.DEFAULT_TYPE):
    # simple text handler so you can ask for help
    await update.message.reply_text("Send me a voice message or an audio file and I'll transcribe it.")

def main():
    app = ApplicationBuilder().token(TELEGRAM_TOKEN).build()

    # Handlers: voice notes, audio files, documents of type audio
    voice_filter = filters.VOICE | filters.AUDIO | (filters.Document.MIME_TYPE("audio/ogg") | filters.Document.MIME_TYPE("audio/mpeg") | filters.Document.MIME_TYPE("audio/*"))
    app.add_handler(MessageHandler(voice_filter, handle_voice))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_text))

    print("Starting bot (long polling)...")
    app.run_polling()

if __name__ == "__main__":
    main()
MODEL_NAME = "small"   # small/medium/base/large — choose by accuracy/speed/size
# ======================

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Load Whisper model once at startup
print("Loading Whisper model (this can take a bit)...")
model = whisper.load_model(MODEL_NAME)
print("Model loaded.")

async def transcribe_file(audio_path: str) -> str:
    """
    Run whisper transcription on a file path. Returns the transcript text.
    """
    # whisper library expects audio in common container; we convert in handler
    result = model.transcribe(audio_path)
    text = result.get("text", "").strip()
    return text

def convert_to_wav(input_path: str, output_path: str):
    """
    Use ffmpeg to convert input audio to PCM WAV with 16k-48k sample rate that whisper accepts.
    """
    cmd = [
        "ffmpeg",
        "-y",
        "-i", input_path,
        "-ar", "16000",        # sample rate
        "-ac", "1",            # mono
        "-c:a", "pcm_s16le",
        output_path
    ]
    logger.info("Running ffmpeg: %s", " ".join(cmd))
    subprocess.run(cmd, check=True)

async def handle_voice(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Handles voice notes and audio files.
    """
    message = update.effective_message
    sender = message.from_user
    logger.info("Received message from %s: %s", sender.username if sender else "unknown", message.message_id)

    # Choose file: voice (voice note) takes precedence, else audio attachments
    file_obj = None
    if message.voice:
        file_obj = message.voice.get_file()
        filename_hint = "voice.oga"
    elif message.audio:
        file_obj = message.audio.get_file()
        filename_hint = message.audio.file_name or "audio"
    elif message.document and message.document.mime_type and message.document.mime_type.startswith("audio"):
        file_obj = message.document.get_file()
        filename_hint = message.document.file_name or "audio"
    else:
        await message.reply_text("I didn't find an audio attachment in that message. Send a voice note or audio file.")
        return

    # Create temp files
    with tempfile.TemporaryDirectory() as tmpdir:
        incoming_path = os.path.join(tmpdir, filename_hint)
        wav_path = os.path.join(tmpdir, "converted.wav")

        # Download the file
        await file_obj.download_to_drive(incoming_path)
        logger.info("Downloaded file to %s", incoming_path)

        # Convert to WAV suitable for whisper
        try:
            convert_to_wav(incoming_path, wav_path)
        except Exception as e:
            logger.exception("ffmpeg conversion failed: %s", e)
            await message.reply_text("Failed to convert audio file. Make sure ffmpeg is installed.")
            return

        await message.reply_text("Transcribing now... (this might take a moment)")

        # Run whisper transcription
        try:
            transcript = await context.application.run_in_executor(None, lambda: model.transcribe(wav_path).get("text", "").strip())
            if not transcript:
                transcript = "[No speech detected or transcription returned empty text]"
        except Exception as e:
            logger.exception("Whisper failed: %s", e)
            await message.reply_text("Transcription failed. See logs.")
            return

        # Send back the transcript
        # If the transcript is long you could instead write to a text file and send as document.
        max_len_for_message = 4000
        if len(transcript) <= max_len_for_message:
            await message.reply_text(f"Transcript:\n\n{transcript}")
        else:
            # Send as file when it's very long
            txt_path = os.path.join(tmpdir, "transcript.txt")
            with open(txt_path, "w", encoding="utf-8") as f:
                f.write(transcript)
            await message.reply_document(open(txt_path, "rb"), filename="transcript.txt")

async def handle_text(update: Update, context: ContextTypes.DEFAULT_TYPE):
    # simple text handler so you can ask for help
    await update.message.reply_text("Send me a voice message or an audio file and I'll transcribe it.")

def main():
    app = ApplicationBuilder().token(TELEGRAM_TOKEN).build()

    # Handlers: voice notes, audio files, documents of type audio
    voice_filter = filters.VOICE | filters.AUDIO | (filters.Document.MIME_TYPE("audio/ogg") | filters.Document.MIME_TYPE("audio/mpeg") | filters.Document.MIME_TYPE("audio/*"))
    app.add_handler(MessageHandler(voice_filter, handle_voice))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_text))

    print("Starting bot (long polling)...")
    app.run_polling()

if __name__ == "__main__":
    main()
