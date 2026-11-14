
https://github.com/user-attachments/assets/e4a7849d-6250-4201-8aad-92442db47cf8
# Hesap Kitap

Küçük işletmeler için fatura ve cari takip uygulaması. Trendyol E-Faturam ile e-Arşiv / e-Fatura keser; kimin ne kadar borcu olduğunu, çek ve senetleri, tedarikçi borçlarını tek yerden gösterir. Arayüz Türkçedir.

https://github.com/user-attachments/assets/34dcf34f-78a7-4fa8-ac8c-954bfee909a3

## Özellikler

- Müşteri (cari) ve ürün kaydı, fatura taslağı ve gönderimi, gelen e-Faturalar
- Tahsilat, çek/senet takibi (vade, karşılıksız, ciro) ve fotoğraf/belge ekleme
- Tedarikçi alım, ödeme ve borç ekstresi
- Pano ve raporlar: alacak yaşlandırma, çek takvimi ve nakit akışı, ödeme alışkanlığı, Excel çıktıları

## Teknoloji

Go · PostgreSQL · templ + HTMX + Tailwind · River (arka plan işleri) · Docker

## Çalıştırma

Go, [Task](https://taskfile.dev) ve Docker gerekir.

```bash
task tools                  # araçları kur
cp .env.example .env        # değerleri doldur
task dev                    # http://localhost:8090
task user:create -- ornek@mail.com
```

Test ve kontrol için `task check`.
