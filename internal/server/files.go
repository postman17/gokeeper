package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kmorozov/gophkeeper/internal/crypto"
	"github.com/kmorozov/gophkeeper/internal/server/storage"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

// lengthPrefixSize is the size of the big-endian uint32 frame prefix that
// delimits successive encrypted chunks inside a stored blob.
const lengthPrefixSize = 4

// chunkSource yields successive plaintext chunks of a file being uploaded.
type chunkSource interface {
	// Next returns the next chunk; io.EOF marks the end of the stream.
	Next() ([]byte, error)
}

// chunkSink consumes plaintext chunks of a file being downloaded.
type chunkSink interface {
	// SendChunk processes the next decrypted chunk.
	SendChunk(chunk []byte) error
}

// uploadStream adapts a gRPC upload stream to chunkSource.
type uploadStream struct {
	stream pb.GophKeeper_UploadFileServer
}

// Next reads the next chunk message from the client stream.
func (u *uploadStream) Next() ([]byte, error) {
	req, err := u.stream.Recv()
	if err == io.EOF {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	switch data := req.GetData().(type) {
	case *pb.UploadFileRequest_Chunk:
		return data.Chunk, nil
	case *pb.UploadFileRequest_Meta:
		return nil, status.Error(codes.InvalidArgument, "unexpected meta message in chunk stream")
	default:
		return nil, status.Error(codes.InvalidArgument, "upload message must carry meta or chunk")
	}
}

// sealStreamToBlob reads plaintext chunks from src, seals each chunk with the
// user's DEK and writes them to dst as length-prefixed nonce||ciphertext
// frames. It returns the total plaintext size written.
func sealStreamToBlob(dek []byte, src chunkSource, dst io.Writer) (int64, error) {
	var total int64
	var frame [lengthPrefixSize]byte
	for {
		chunk, err := src.Next()
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
		if len(chunk) == 0 {
			continue
		}
		blob, err := crypto.Encrypt(dek, chunk)
		if err != nil {
			return total, fmt.Errorf("encrypt chunk: %w", err)
		}
		binary.BigEndian.PutUint32(frame[:], uint32(len(blob)))
		if _, err := dst.Write(frame[:]); err != nil {
			return total, fmt.Errorf("write frame header: %w", err)
		}
		if _, err := dst.Write(blob); err != nil {
			return total, fmt.Errorf("write frame: %w", err)
		}
		total += int64(len(chunk))
	}
}

// openBlobToStream reads length-prefixed encrypted frames from src, decrypts
// each with the user's DEK and pushes the plaintext chunks into sink.
func openBlobToStream(dek []byte, src io.Reader, sink chunkSink) error {
	var header [lengthPrefixSize]byte
	for {
		if _, err := io.ReadFull(src, header[:]); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("read frame header: %w", err)
		}
		size := binary.BigEndian.Uint32(header[:])
		blob := make([]byte, size)
		if _, err := io.ReadFull(src, blob); err != nil {
			return fmt.Errorf("read frame: %w", err)
		}
		chunk, err := crypto.Decrypt(dek, blob)
		if err != nil {
			return status.Error(codes.Internal,
				fmt.Sprintf("decrypt file chunk: %v (server master password may not match the one used at upload)", err))
		}
		if err := sink.SendChunk(chunk); err != nil {
			return err
		}
	}
}

// uploadResult carries the outcome of the streaming seal loop.
type uploadResult struct {
	size int64
	err  error
}

// fileKey builds the blob store key for a user's file.
func fileKey(userID, fileID uuid.UUID) string {
	return fmt.Sprintf("files/%s/%s", userID, fileID)
}

// UploadFile streams a file from the client, encrypts every chunk with the
// user's DEK and stores the sealed blob in the object storage. The database
// record is created only after a successful upload; on any failure the stored
// blob is removed as compensation.
func (s *GophKeeper) UploadFile(stream pb.GophKeeper_UploadFileServer) error {
	ctx := stream.Context()
	userID, err := s.userID(ctx)
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "file metadata is required")
	}
	if err != nil {
		return err
	}
	metaMsg := first.GetMeta()
	if metaMsg == nil {
		return status.Error(codes.InvalidArgument, "first upload message must carry file metadata")
	}
	if metaMsg.GetName() == "" {
		return status.Error(codes.InvalidArgument, "file name is required")
	}
	dek, err := s.getUserDEK(ctx, userID)
	if err != nil {
		return err
	}
	fileID := uuid.New()
	key := fileKey(userID, fileID)

	pr, pw := io.Pipe()
	uploadDone := make(chan uploadResult, 1)
	go func() {
		n, err := sealStreamToBlob(dek, &uploadStream{stream: stream}, pw)
		// Closing with an error aborts the reader side of the pipe.
		_ = pw.CloseWithError(err)
		uploadDone <- uploadResult{size: n, err: err}
	}()
	putErr := s.blobs.Put(ctx, key, pr)
	// The storage call may return before the whole stream is consumed
	// (e.g. on an early error). Close the reader side so the sealing
	// goroutine never blocks on a pipe nobody reads.
	_ = pr.CloseWithError(io.ErrClosedPipe)
	result := <-uploadDone
	if putErr != nil {
		_ = s.blobs.Delete(ctx, key)
		return status.Errorf(codes.Internal, "store file blob: %v", putErr)
	}
	if result.err != nil {
		_ = s.blobs.Delete(ctx, key)
		if _, ok := status.FromError(result.err); ok {
			return result.err
		}
		return status.Error(codes.Internal, "upload file")
	}
	_, err = s.store.CreateFile(ctx, storage.File{
		ID:     fileID,
		UserID: userID,
		Name:   metaMsg.GetName(),
		Size:   result.size,
		Meta:   metaMsg.GetMeta(),
		S3Key:  key,
	})
	if err != nil {
		_ = s.blobs.Delete(ctx, key)
		return status.Error(codes.Internal, "store file")
	}
	return stream.SendAndClose(&pb.UploadFileResponse{Id: fileID.String()})
}

// downloadStream adapts a gRPC download stream to chunkSink.
type downloadStream struct {
	stream pb.GophKeeper_DownloadFileServer
}

// SendChunk forwards a decrypted chunk to the client stream.
func (d *downloadStream) SendChunk(chunk []byte) error {
	return d.stream.Send(&pb.DownloadFileResponse{
		Data: &pb.DownloadFileResponse_Chunk{Chunk: chunk},
	})
}

// DownloadFile streams a stored file back to the client, decrypting chunk
// frames with the user's DEK on the fly.
func (s *GophKeeper) DownloadFile(req *pb.DownloadFileRequest, stream pb.GophKeeper_DownloadFileServer) error {
	ctx := stream.Context()
	userID, err := s.userID(ctx)
	if err != nil {
		return err
	}
	id, err := parseID(req.GetId())
	if err != nil {
		return err
	}
	file, err := s.store.GetFile(ctx, userID, id)
	if err == storage.ErrNotFound {
		return status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		return status.Error(codes.Internal, "load file")
	}
	dek, err := s.getUserDEK(ctx, userID)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.DownloadFileResponse{
		Data: &pb.DownloadFileResponse_Meta{Meta: &pb.FileMeta{
			Name: file.Name,
			Size: file.Size,
			Meta: file.Meta,
		}},
	}); err != nil {
		return err
	}
	blob, err := s.blobs.Get(ctx, file.S3Key)
	if err != nil {
		return status.Error(codes.Internal, "load file blob")
	}
	defer blob.Close()
	return openBlobToStream(dek, blob, &downloadStream{stream: stream})
}

// ListFiles returns the file records of the current user without content.
func (s *GophKeeper) ListFiles(ctx context.Context, _ *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	files, err := s.store.ListFiles(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "load files")
	}
	resp := &pb.ListFilesResponse{Files: make([]*pb.FileInfo, 0, len(files))}
	for _, f := range files {
		resp.Files = append(resp.Files, &pb.FileInfo{
			Id:        f.ID.String(),
			Name:      f.Name,
			Size:      f.Size,
			Meta:      f.Meta,
			UpdatedAt: timestamppb.New(f.UpdatedAt),
		})
	}
	return resp, nil
}

// DeleteFile removes the file record and its stored blob.
func (s *GophKeeper) DeleteFile(ctx context.Context, req *pb.DeleteFileRequest) (*pb.DeleteFileResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.GetId())
	if err != nil {
		return nil, err
	}
	file, deleted, err := s.store.DeleteFile(ctx, userID, id)
	if err != nil {
		return nil, status.Error(codes.Internal, "delete file")
	}
	if !deleted {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err := s.blobs.Delete(ctx, file.S3Key); err != nil {
		return nil, status.Error(codes.Internal, "delete file blob")
	}
	return &pb.DeleteFileResponse{Ok: true}, nil
}
