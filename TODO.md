# Bilinen Sorunlar / Yapılacaklar

## Öncelikli — veri bozulmasına yol açan hata

- **Eşzamanlı dosya gönderimi/alımı veriyi bozar.** `/send` komutu her
  çağrıldığında ayrı bir goroutine başlatıyor
  ([internal/ui/model.go](internal/ui/model.go)) ve protokolde bir transfer
  ID'si yok — `TypeFileChunk` payload'ı düz bayt dizisi. Art arda iki
  `/send` çalıştırılırsa (ya da bir gönderim sürerken karşı taraf da bir
  şey gönderirse) chunk'lar birbirine karışır. Alıcı tarafta da yeni bir
  `TypeFileHeader` gelince önceki transfer hâlâ açıksa
  ([internal/ui/filetransfer.go:77](internal/ui/filetransfer.go:77)) eski
  dosya kapatılmadan üzerine yazılıyor — hem bozulma hem dosya handle
  sızıntısı.
  **Çözüm önerisi:** protokole transfer ID ekle (plan dosyasındaki
  `pure-shimmying-seahorse.md`'de zaten tasarlanmıştı), gönderim tarafında
  aynı anda tek transfere izin ver (kilit/kuyruk), alıcı tarafta ID
  eşleşmiyorsa reddet.

## Bilinen sınırlamalar (kapsam dışı bırakıldı, ileride değerlendirilebilir)

- Bütünlük kontrolü sadece boyut karşılaştırması yapıyor, SHA-256 hash
  doğrulaması yok.
- Gelen dosya, çalışma dizinindeki aynı isimli dosyanın üzerine sessizce
  yazılıyor (üzerine yazmadan önce onay yok, otomatik yeniden adlandırma
  yok).
- Bağlantı koptuğunda yeniden bağlanma yok; devam eden bir transfer varsa
  iptal/temizlik mekanizması yok.
- Test kapsamı dar: yalnızca `internal/protocol` unit testleri var;
  `internal/config`, `internal/transport`, `internal/ui` için test yok.
