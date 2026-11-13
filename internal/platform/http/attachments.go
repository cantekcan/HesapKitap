package httpserver

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"hesapkitap/internal/attachment"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
)

// canWriteAttachment: tahsilat/çek kayıtlarına tahsilat veya çek yetkisi olan, tedarikçi kayıtlarına tedarikçi yetkisi olan ekleyebilir.
func canWriteAttachment(u auth.User, o attachment.Owner) bool {
	switch o {
	case attachment.OwnerPayment, attachment.OwnerCheque:
		return u.Can(auth.PermPaymentCreate) || u.Can(auth.PermChequeManage)
	case attachment.OwnerSupplierEntry, attachment.OwnerSupplierPayment:
		return u.Can(auth.PermSupplierWrite)
	}
	return false
}

func attachmentTitle(o attachment.Owner) string {
	switch o {
	case attachment.OwnerCheque:
		return "Çek fotoğrafı / belge"
	case attachment.OwnerPayment:
		return "Tahsilat belgesi (dekont, fotoğraf)"
	case attachment.OwnerSupplierEntry:
		return "Alım belgesi (fatura, irsaliye, fotoğraf)"
	case attachment.OwnerSupplierPayment:
		return "Ödeme belgesi (dekont, çek fotoğrafı)"
	}
	return "Belgeler"
}

func (s *server) attachmentPanelData(r *http.Request, o attachment.Owner, id uuid.UUID, errMsg string) (views.AttachmentPanel, error) {
	items, err := s.Attachments.List(r.Context(), o, id)
	if err != nil {
		return views.AttachmentPanel{}, err
	}
	me, _ := auth.CurrentUser(r.Context())
	return views.AttachmentPanel{OwnerType: string(o), OwnerID: id.String(), Title: attachmentTitle(o), Items: items,
		CanWrite: canWriteAttachment(me, o), Error: errMsg}, nil
}

func (s *server) attachmentPanel(w http.ResponseWriter, r *http.Request) {
	o, id := attachment.Owner(r.URL.Query().Get("type")), r.URL.Query().Get("id")
	oid, err := uuid.Parse(id)
	if !o.Valid() || err != nil {
		http.Error(w, "geçersiz istek", http.StatusBadRequest)
		return
	}
	p, err := s.attachmentPanelData(r, o, oid, "")
	if err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.AttachmentModal(p))
}

func (s *server) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize+(1<<20))
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		s.attachmentBox(w, r, "", uuid.Nil, "Dosya okunamadı ya da 10 MB'tan büyük.")
		return
	}
	o := attachment.Owner(r.FormValue("type"))
	oid, err := uuid.Parse(r.FormValue("id"))
	if !o.Valid() || err != nil {
		http.Error(w, "geçersiz istek", http.StatusBadRequest)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	if !canWriteAttachment(me, o) {
		http.Error(w, "Bu işlem için yetkiniz yok.", http.StatusForbidden)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		s.attachmentBox(w, r, o, oid, "Dosya seçilmedi.")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, attachment.MaxSize+1))
	if err != nil {
		s.attachmentBox(w, r, o, oid, "Dosya okunamadı.")
		return
	}
	ct, err := attachment.Detect(data)
	if err != nil {
		s.attachmentBox(w, r, o, oid, err.Error()+".")
		return
	}
	name := attachment.CleanName(hdr.Filename)
	id, err := s.Attachments.Add(r.Context(), attachment.File{
		Meta: attachment.Meta{OwnerType: o, OwnerID: oid, Filename: name, ContentType: ct, Size: len(data), UploadedBy: me.Email}, Data: data})
	if err != nil {
		s.serverError(w, r, "dosya kaydedilemedi", err)
		return
	}
	s.record(r, "attachment.add", string(o), oid.String(), name+" ("+id.String()[:8]+")")
	s.attachmentBox(w, r, o, oid, "")
}

// attachmentBox answers the HTMX request with the refreshed file box.
func (s *server) attachmentBox(w http.ResponseWriter, r *http.Request, o attachment.Owner, oid uuid.UUID, errMsg string) {
	p := views.AttachmentPanel{Error: errMsg}
	if o != "" {
		var err error
		if p, err = s.attachmentPanelData(r, o, oid, errMsg); err != nil {
			s.serverError(w, r, "ekler okunamadı", err)
			return
		}
	}
	render(w, r, http.StatusOK, views.AttachmentBox(p))
}

func (s *server) attachmentGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	f, err := s.Attachments.Get(r.Context(), id)
	if errors.Is(err, attachment.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "dosya okunamadı", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": f.Filename}))
	h.Set("Content-Length", strconv.Itoa(len(f.Data)))
	h.Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(f.Data)
}

func (s *server) attachmentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	f, err := s.Attachments.Get(r.Context(), id)
	if errors.Is(err, attachment.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "dosya okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	if !canWriteAttachment(me, f.OwnerType) {
		http.Error(w, "Bu işlem için yetkiniz yok.", http.StatusForbidden)
		return
	}
	o, oid, err := s.Attachments.Delete(r.Context(), id)
	if err != nil && !errors.Is(err, attachment.ErrNotFound) {
		s.serverError(w, r, "dosya silinemedi", err)
		return
	}
	s.record(r, "attachment.delete", string(f.OwnerType), f.OwnerID.String(), f.Filename)
	if o == "" {
		o, oid = f.OwnerType, f.OwnerID
	}
	s.attachmentBox(w, r, o, oid, "")
}
