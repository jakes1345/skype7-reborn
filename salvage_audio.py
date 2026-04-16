import os

src = "dissection/resources/.rsrc/RCDATA"
audio_dest = "assets/sounds"
os.makedirs(audio_dest, exist_ok=True)

for filename in os.listdir(src):
    path = os.path.join(src, filename)
    if os.path.isdir(path):
        continue
    
    with open(path, "rb") as f:
        data = f.read(2048) # Read a chunk
        
    ext = None
    if b"RIFF" in data and b"WAVE" in data:
        ext = "wav"
    elif b"OggS" in data:
        ext = "ogg"
    elif b"ID3" in data or b"\xFF\xFB" in data:
        ext = "mp3"
        
    if ext:
        target = os.path.join(audio_dest, f"extracted_{filename}.{ext}")
        with open(path, "rb") as f:
            with open(target, "wb") as out:
                out.write(f.read())
        print(f"Salvaged: {target}")
