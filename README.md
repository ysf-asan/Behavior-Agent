# behavior-agent

Windows için bir davranışsal biyometri ajanı. Klavye ve fare kullanım alışkanlıklarını öğrenir, profilden sapan hareketleri tespit eder.

## Nasıl çalışır

Her 15 saniyede bir 44 farklı öznitelik çıkarır (tuş basma süreleri, fare hızı, scroll düzeni, tıklama tipleri). 30 örnek toplandığında Isolation Forest ile bir model eğitir ve sürekli izleme başlatır. Model saf Go ile yazıldı — harici bir ML runtime'ı gerekmez.

## Bağımlılıklar

- Windows 10/11
- Yalnızca Win32 API kullanır, CGO veya ek runtime yok

## Kurulum

```
git clone https://github.com/ysf-asan/behavior-agent
cd behavior-agent
go build -o agent.exe ./cmd/agent
./agent.exe
```

Varsayılan port 9090, DB dosyası binary'nin yanında oluşur.

## Ortam değişkenleri

- `API_PORT` — REST API portu (varsayılan: 9090)
- `DB_PATH` — SQLite veritabanı yolu

## API

| Yöntem | Path | İşlevi |
|--------|------|--------|
| GET | /api/status | Ajan durumu, öğrenme/izleme modu |
| GET | /api/profile | Eğitilmiş profil bilgisi |
| POST | /api/train | Manuel eğitim başlatma |
| GET | /api/risk | Son anomali skoru |
| GET | /api/events | Geçmiş olaylar |
| GET/POST | /api/startup | Windows başlangıç ayarı |

## Lisans

MIT
