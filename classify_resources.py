import os
import shutil

src = "dissection/resources/.rsrc/RCDATA"
dest = "dissection/categories"

os.makedirs(dest, exist_ok=True)

for filename in os.listdir(src):
    path = os.path.join(src, filename)
    if os.path.isdir(path):
        continue
    
    try:
        with open(path, "rb") as f:
            header = f.read(1024)
            
        ext = "bin"
        if header.startswith(b"\x89PNG"):
            ext = "png"
        elif header.startswith(b"GIF8"):
            ext = "gif"
        elif header.startswith(b"BM"):
            ext = "bmp"
        elif header.startswith(b"\xff\xd8"):
            ext = "jpg"
        elif b"WAVE" in header and b"fmt " in header:
            ext = "wav"
        elif b"<?xml" in header or b"<UI" in header or b"<Template" in header:
            ext = "xml"
        elif header.startswith(b"PK\x03\x04"):
            ext = "zip"
        elif b"CURS" in header:
            ext = "cur"
        
        # Check if it looks like plain text
        if ext == "bin":
            try:
                text = header.decode('utf-8', errors='ignore')
                if "<" in text and ">" in text:
                    ext = "txt_xml"
            except:
                pass

        cat_dir = os.path.join(dest, ext)
        os.makedirs(cat_dir, exist_ok=True)
        shutil.copy(path, os.path.join(cat_dir, filename + "." + ext))
    except Exception as e:
        print(f"Error processing {filename}: {e}")

print("Categorization complete.")
