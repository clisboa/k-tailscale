# Native widget previews

These PNGs were captured on 2026-10-31 from the actual `tailui::Panel` widget in
`src/panel.cc`, rendered with Qt 5.15 on Linux. They are native UI previews, not
photographs or screenshots from physical Kobo hardware. Production targets
Qt 5.2.1; platform fonts and rendering can differ slightly.

- `widget-connected.png`: 760 × 415, Connected with the Updates section closed.
- `widget-updates.png`: 760 × 821, Connected with the Updates section expanded.

Both captures use synthetic local status: hostname `kobo-libra-colour`, example
IP `100.64.0.8`, Tailscale 1.104.4 and addon 0.1.8, with no pending updates.
They include no real account information, credentials or sign-in links.
The actual widget was captured directly with `QWidget::grab()` at its natural
size, including its dropdown pointer. The images have not been retouched.

The README and installer share these assets. The site builder copies the PNGs
and omits this provenance file. Refresh the previews when the visible widget
changes; capture only stable idle states with synthetic data.
