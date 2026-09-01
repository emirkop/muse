package interfaces

import (
	"errors"
	"net/http"
	"strconv"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
	"muse-backend/internal/platform/observability"
)

const defaultAppAssetVersion = 1

const maxAppAssetVersion = 1000

func (h *Handlers) HandleBundleManifest(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}
	if h.bundles == nil {
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventAssetDeliveryUnconfigured,
			Category: observability.CategoryAssetDelivery,
			Outcome:  observability.OutcomeUnavailable,
			Reason:   observability.ReasonNotConfigured,
		})
		writeError(w, http.StatusServiceUnavailable, "asset bundle delivery is not configured")
		return
	}

	appAssetVersion, ok := appAssetVersionFrom(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid app_asset_version")
		return
	}

	manifest, err := h.bundles.Manifest(r.Context(), r.PathValue("bundleID"), appAssetVersion)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrBundleNotFound):
		observability.LogWith(r.Context(), h.logger, observability.Event{
			Name:     observability.EventAssetBundleNotPublished,
			Category: observability.CategoryAssetDelivery,
			Outcome:  observability.OutcomeRefused,
			Reason:   observability.ReasonNotFound,
		},
			"bundle_id", r.PathValue("bundleID"),
			"app_asset_version", appAssetVersion,
		)
		writeError(w, http.StatusNotFound, "asset bundle not found")
		return
	case errors.Is(err, application.ErrBundleDeliveryUnconfigured):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventAssetDeliveryUnconfigured,
			Category: observability.CategoryAssetDelivery,
			Outcome:  observability.OutcomeUnavailable,
			Reason:   observability.ReasonNotConfigured,
		})
		writeError(w, http.StatusServiceUnavailable, "asset bundle delivery is not configured")
		return
	case errors.Is(err, domain.ErrBundleInvalid):
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventAssetPublishFailed,
			Category: observability.CategoryAssetDelivery,
			Outcome:  observability.OutcomeFailed,
			Err:      err,
		})
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, newBundleManifestResponse(manifest))
}

func (h *Handlers) HandleGetRoomVariant(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	variant, found, err := h.catalog.FindVariant(r.Context(), domain.VariantID(r.PathValue("variantID")))
	if err != nil {
		h.logger.Error("catalog find variant failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "room variant not found")
		return
	}
	writeJSON(w, http.StatusOK, newVariantResponse(variant))
}

func appAssetVersionFrom(r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("app_asset_version")
	if raw == "" {
		return defaultAppAssetVersion, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maxAppAssetVersion {
		return 0, false
	}
	return parsed, true
}
