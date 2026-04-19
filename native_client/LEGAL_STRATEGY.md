# Phaze: Forensic Reconstruction Legal Strategy

To ensure the long-term survival of the Phaze project and protect it from DMCA takedowns or trademark litigation from Microsoft (Skype), we adhere to a strict **AeroShield Isolation Protocol**.

## 1. Clean-Room Implementation (The Chinese Wall)
The Phaze client application is a 100% original Go-lang implementation. 
- **The "Dirty" Logic**: Any analysis of the original Skype 7.41 protocol (MSNP24) or resource layout is performed by diagnostic scripts (AeroSlicer) in the `/dissection` directory.
- **The "Clean" Code**: The engineers (AI & User) developing the Phaze client work exclusively from functional specifications. No code is copied or "inspired" by the original Skype source or disassembled binaries.

## 2. Zero-Asset Distribution (Scribing Policy)
To avoid copyright infringement, the Phaze repository contains **ZERO** copyrighted assets from Microsoft.
- All Skype icons, sounds, and UI sprites are extracted locally on the user's machine from an original `SkypeSetup.exe` provided by the user.
- Our build process uses the `AeroSlicer` tool to "Scribe" these assets into the `/assets` folder, ensuring we never distribute copyrighted files.

## 3. Brand Independence
- **Name**: Phaze (formerly Skype 7 Reborn).
- **Protocol**: Phaze Nexus (A decentralized relay protocol).
- **Logo**: A unique "Arrow" or "Flame" logo replace the Skype blue cloud.

## 4. Legals & Disclaimers
This project is for educational preservation and interoperability research. We assert our rights under:
- **US Copyright Act § 1201 (f)**: Reverse engineering for the purpose of achieving interoperability.
- **EC Software Directive 2009/24/EC**: Decompilation for interoperability.

## 5. Defense against "Look & Feel"
While we emulate the classic layout for preservation, we justify this under the "Scènes à faire" doctrine—certain UI elements are standard for communication software and mandated by the functional goal of reconstructing a retired interface for historical study.
