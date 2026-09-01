package interfaces

import (
	"time"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/domain"
)

type initiatePhotoUploadRequest struct {
	ClientUploadID string `json:"client_upload_id"`
	ContentType    string `json:"content_type"`
	ByteSize       int64  `json:"byte_size"`
	PixelWidth     int    `json:"pixel_width"`
	PixelHeight    int    `json:"pixel_height"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

func (r initiatePhotoUploadRequest) declaration() domain.PhotoDeclaration {
	return domain.PhotoDeclaration{
		ClientUploadID: r.ClientUploadID,
		ContentType:    r.ContentType,
		ByteSize:       r.ByteSize,
		PixelWidth:     r.PixelWidth,
		PixelHeight:    r.PixelHeight,
		ChecksumSHA256: r.ChecksumSHA256,
	}
}

type uploadInstructions struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type initiatePhotoUploadResponse struct {
	AssetID string              `json:"asset_id"`
	State   string              `json:"state"`
	Upload  *uploadInstructions `json:"upload"`
}

func newInitiatePhotoUploadResponse(ticket application.PhotoUploadTicket) initiatePhotoUploadResponse {
	resp := initiatePhotoUploadResponse{
		AssetID: string(ticket.Asset.ID),
		State:   string(ticket.Asset.State),
	}
	if ticket.Upload != nil {
		resp.Upload = &uploadInstructions{
			URL:       ticket.Upload.URL,
			Method:    ticket.Upload.Method,
			Headers:   ticket.Upload.Headers,
			ExpiresAt: ticket.Upload.ExpiresAt,
		}
	}
	return resp
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}
