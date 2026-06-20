package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error parsing data", err)
		return
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse from file", err)
		return
	}
	defer file.Close()

	mediaType := header.Header.Get("Content-Type")

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video from database", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "UserIDs do not match", nil)
		return
	}

	fileExtension := ""
	if strings.Contains(mediaType, "png") {
		fileExtension = ".png"
	} else if strings.Contains(mediaType, "jpeg") {
		fileExtension = ".jpeg"
	}

	thumbnailFilepath := fmt.Sprintf("%v.%v", video.ID, fileExtension)
	finalFilepath := filepath.Join(cfg.assetsRoot, thumbnailFilepath)
	thumbnailURL := fmt.Sprintf("http://localhost:%v/assets/%v", cfg.port, thumbnailFilepath)
	video.ThumbnailURL = &thumbnailURL
	fileThumbnail, err := os.Create(finalFilepath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not create file", err)
	}
	io.Copy(fileThumbnail, file)

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
