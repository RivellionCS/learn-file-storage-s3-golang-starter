package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

type VideoInfo struct {
	Streams []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"streams"`
}

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1 << 30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error parsing videoID", err)
		return
	}

	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error getting token", err)
		return
	}

	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error validating user", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error finding video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "userIDs for the current user and the video user do not match", err)
		return
	}

	multipartVideo, multipartHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error getting file from video key", err)
		return
	}
	defer multipartVideo.Close()

	contentType := multipartHeader.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error parsing media type", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "error media type is nota mp4 video", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not create temp video file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, multipartVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not copy file to temp file", err)
		return
	}

	tempFile.Seek(0, io.SeekStart)

	byteSize := [32]byte{}
	_, err = rand.Read(byteSize[:])
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not generate random filename", err)
		return
	}

	vidPrefix, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not determine video dimensions type", err)
		return
	}

	pathPrefix := ""
	if vidPrefix == "16:9" {
		pathPrefix = "landscape"
	} else if vidPrefix == "9:16" {
		pathPrefix = "portrait"
	} else {
		pathPrefix = vidPrefix
	}

	fileNameKey := fmt.Sprintf("%v/%v.mp4", pathPrefix, hex.EncodeToString(byteSize[:]))

	putObjectParams := s3.PutObjectInput {
		Bucket: aws.String(cfg.s3Bucket),
		Key: aws.String(fileNameKey),
		Body: tempFile,
		ContentType: aws.String(mediaType),
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &putObjectParams)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error creating aws s3 object", err)
		return
	}

	uploadVideoUrl := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, fileNameKey)

	video.VideoURL = &uploadVideoUrl

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var b bytes.Buffer

	cmd.Stdout = &b

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	vidInfo := VideoInfo{}

	err = json.Unmarshal(b.Bytes(), &vidInfo)
	if err != nil {
		return "", err
	}

	if len(vidInfo.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	ratio := float64(vidInfo.Streams[0].Width) / float64(vidInfo.Streams[0].Height)

	target := 16.0 / 9.0
	tolerance := 0.1
	if math.Abs(ratio - target) < tolerance {
		return "16:9", nil
	}

	target = 9.0 / 16
	if math.Abs(ratio - target) < tolerance {
		return "9:16", nil
	}

	return "other", nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := fmt.Sprintf("%v.processing", filePath)

	cmd := exec.Command("ffmpeg", "-c", "copy", "-movfiles", "faststart", "-f", "mp4", outputFilePath)

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilePath, nil
}