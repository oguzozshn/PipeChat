# PipeChat

İki makine arasında SSH ile kimlik doğrulamalı, simetrik metin mesajlaşma ve
dosya transferi sağlayan bir terminal uygulaması.

## Özellikler

- **Simetrik mimari** — aynı binary hem `listen` hem `connect` modunda
  çalışır; bağlantı kurulduktan sonra iki taraf da aynı protokolü konuşur.
- **ed25519 public-key authentication** — parola yok, sadece anahtar çifti.
- **Trust-on-first-use (TOFU) host doğrulaması** — ilk bağlantıda karşı
  tarafın host key'i kaydedilir, sonraki bağlantılarda karşılaştırılır.
- **Tek SSH kanalı üzerinde multiplex edilen metin + dosya transferi** — ekstra
  port veya SFTP alt sistemi gerekmez.
- **Bubble Tea terminal arayüzü** — kayan sohbet akışı ve dosya transferi
  sırasında ilerleme çubuğu.

## Kurulum

```bash
go build -o pipechat ./cmd/pipechat
```

## Kullanım

### 1. Anahtarları değiştirin

Her iki makinede de kendi public key'inizi yazdırın:

```bash
./pipechat pubkey
```

Karşı tarafın döndürdüğü satırı diğer makinede yetkilendirin:

```bash
./pipechat authorize "ssh-ed25519 AAAA..."
```

### 2. Bağlanın

Bir makinede dinlemeye başlayın:

```bash
./pipechat listen --addr :2222
```

Diğer makineden bağlanın:

```bash
./pipechat connect --addr <host>:2222
```

### 3. Sohbet edin

- Metin göndermek için yazıp Enter'a basın.
- Dosya göndermek için: `/send /path/to/file`
- Çıkmak için `Ctrl+C` veya `Esc`.

Gelen dosyalar, uygulamanın çalıştırıldığı dizine kaydedilir.

## Yapılandırma dosyaları

Anahtarlar ve güven listeleri `~/.pipechat` altında tutulur:

| Dosya               | Amaç                                             |
|---------------------|--------------------------------------------------|
| `id_ed25519`        | `connect` modunda kullanılan istemci kimliği      |
| `host_key`          | `listen` modunda kullanılan sunucu kimliği        |
| `authorized_keys`   | `listen` tarafının kabul ettiği public key'ler    |
| `known_hosts`       | `connect` tarafının TOFU ile güvendiği host key'ler |

## Geliştirme

```bash
go build ./...
go vet ./...
go test ./...
```

## Mimari

- `internal/transport` — SSH server/client kurulumu, `pipechat-data` kanalı.
- `internal/protocol` — length-prefixed frame formatı, dosya header/chunk
  encode/decode.
- `internal/config` — anahtar üretimi, `authorized_keys`, TOFU `known_hosts`.
- `internal/ui` — Bubble Tea terminal arayüzü.
