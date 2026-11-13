package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"hesapkitap/internal/money"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/product"
)

const listPerPage = 20

// listParams reads the shared q / status / page query parameters of the list pages.
func listParams(r *http.Request) (q, status string, page int) {
	q = strings.TrimSpace(r.URL.Query().Get("q"))
	status = r.URL.Query().Get("status")
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return q, status, page
}

func pageCount(total int) int { return max(1, (total+listPerPage-1)/listPerPage) }

func isFragment(r *http.Request, target string) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == target
}

func (s *server) productList(w http.ResponseWriter, r *http.Request) {
	q, status, page := listParams(r)
	if status != "active" && status != "passive" {
		status = ""
	}
	items, total, err := s.Products.List(r.Context(), product.ListFilter{Query: q, Status: status, Limit: listPerPage, Offset: (page - 1) * listPerPage})
	if err != nil {
		s.serverError(w, r, "ürün listesi okunamadı", err)
		return
	}
	l := views.ProductList{Items: items, Query: q, Status: status, Page: page, PageCount: pageCount(total), Total: total}
	if isFragment(r, "product-list") {
		render(w, r, http.StatusOK, views.ProductListFragment(l))
		return
	}
	render(w, r, http.StatusOK, views.ProductsPage(s.pageData(r, "products", "Ürünler"), l))
}

func (s *server) productNew(w http.ResponseWriter, r *http.Request) {
	f := views.ProductForm{IsNew: true, Product: product.Product{Unit: "C62", TaxPercent: 20, Active: true}}
	render(w, r, http.StatusOK, views.ProductFormPage(s.pageData(r, "products", "Yeni ürün"), f))
}

func (s *server) productShow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	p, err := s.Products.Get(r.Context(), id)
	if errors.Is(err, product.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "ürün okunamadı", err)
		return
	}
	f := views.ProductForm{Product: p, UnitPrice: money.Format(p.UnitPrice), Saved: r.URL.Query().Get("saved") == "1"}
	render(w, r, http.StatusOK, views.ProductFormPage(s.pageData(r, "products", p.Name), f))
}

func productFromForm(r *http.Request) (product.Product, string, product.FieldErrors) {
	v := r.PostForm.Get
	errs := product.FieldErrors{}
	tax, err := strconv.Atoi(v("tax_percent"))
	if err != nil {
		tax = -1
	}
	price := v("unit_price")
	kurus, err := money.Parse(price)
	if err != nil {
		errs["unit_price"] = "Fiyatı 1.234,56 biçiminde girin."
	}
	p := product.Product{Code: v("code"), Name: v("name"), Unit: v("unit"), UnitPrice: kurus, TaxPercent: tax, Active: v("active") == "1"}
	return p, price, errs
}

func (s *server) productCreate(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	p, price, errs := productFromForm(r)
	if len(errs) == 0 {
		created, verrs, err := s.Products.Create(r.Context(), p)
		if err != nil {
			s.serverError(w, r, "ürün kaydedilemedi", err)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "product.create", "product", created.ID.String(), created.Name)
			http.Redirect(w, r, "/products/"+created.ID.String()+"?saved=1", http.StatusSeeOther) //nolint:gosec // id veritabanından geldi
			return
		}
		errs = verrs
	} else {
		p.Normalize()
		for k, m := range p.Validate() {
			if _, has := errs[k]; !has {
				errs[k] = m
			}
		}
	}
	f := views.ProductForm{IsNew: true, Product: p, Errors: errs, UnitPrice: price}
	render(w, r, http.StatusUnprocessableEntity, views.ProductFormPage(s.pageData(r, "products", "Yeni ürün"), f))
}

func (s *server) productUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	p, price, errs := productFromForm(r)
	p.ID = id
	if len(errs) == 0 {
		verrs, err := s.Products.Update(r.Context(), p)
		if errors.Is(err, product.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, r, "ürün güncellenemedi", err)
			return
		}
		if len(verrs) == 0 {
			http.Redirect(w, r, "/products/"+id.String()+"?saved=1", http.StatusSeeOther) //nolint:gosec // id uuid olarak ayrıştırıldı
			return
		}
		errs = verrs
	}
	f := views.ProductForm{Product: p, Errors: errs, UnitPrice: price}
	render(w, r, http.StatusUnprocessableEntity, views.ProductFormPage(s.pageData(r, "products", p.Name), f))
}
