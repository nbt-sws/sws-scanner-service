package grpc

import (
	"context"
	"fmt"
	"net"

	scannerv1 "github.com/jatibroski/sws-shared-protos/gen/go/scanner/v1"
	"github.com/jatibroski/sws-scanner-service/internal/usecase/scan"
	"github.com/jatibroski/sws-scanner-service/internal/usecase/variants"
	"google.golang.org/grpc"
)

// Server implements scanner.v1.ScannerService.
type Server struct {
	scannerv1.UnimplementedScannerServiceServer
	scanUC     *scan.UseCase
	variantsUC *variants.UseCase
}

// NewServer creates a gRPC server for the scanner domain.
func NewServer(scanUC *scan.UseCase, variantsUC *variants.UseCase) *Server {
	return &Server{scanUC: scanUC, variantsUC: variantsUC}
}

// Register registers the scanner service on the supplied gRPC server.
func (s *Server) Register(gs *grpc.Server) {
	scannerv1.RegisterScannerServiceServer(gs, s)
}

// StartListener starts a TCP listener on addr and serves gRPC.
func (s *Server) StartListener(addr string) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	gs := grpc.NewServer()
	s.Register(gs)
	go func() {
		if err := gs.Serve(lis); err != nil {
			fmt.Printf("grpc server exited: %v\n", err)
		}
	}()
	return gs, nil
}

// Scan identifies a single trading-card image.
func (s *Server) Scan(ctx context.Context, req *scannerv1.ScanRequest) (*scannerv1.ScanResponse, error) {
	resp, err := s.scanUC.Scan(ctx, scan.ScanRequest{
		Image: req.Image,
		TCG:   req.Tcg,
		Lang:  req.Lang,
		Force: req.Force,
	}, "")
	if err != nil {
		return nil, err
	}
	return scanResponseToProto(resp), nil
}

// GetVerifiedCard returns community-verified metadata for a code + rarity.
func (s *Server) GetVerifiedCard(ctx context.Context, req *scannerv1.GetVerifiedCardRequest) (*scannerv1.VerifiedCard, error) {
	if s.variantsUC == nil {
		return nil, fmt.Errorf("variants use case not initialized")
	}
	res := s.variantsUC.OPDetails(ctx, variants.OPDetailsRequest{
		Code:   req.Code,
		Rarity: req.Rarity,
		Lang:   req.Lang,
	})
	if res.Error != "" {
		return nil, fmt.Errorf("%s", res.Error)
	}
	samples := map[string]string{}
	if res.Card != nil {
		if m, ok := res.Card["samples"].(map[string]interface{}); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					samples[k] = s
				}
			}
		}
	}
	return &scannerv1.VerifiedCard{
		DocKey:  safeString(res.Card, "docKey"),
		Code:    req.Code,
		Rarity:  req.Rarity,
		NameEn:  safeString(res.Card, "nameEn"),
		NameJp:  safeString(res.Card, "nameJp"),
		NameCn:  safeString(res.Card, "nameCn"),
		Type:    safeString(res.Card, "type"),
		Samples: samples,
	}, nil
}

// LookupByPHash finds visually similar verified cards.
func (s *Server) LookupByPHash(ctx context.Context, req *scannerv1.PHashLookupRequest) (*scannerv1.PHashLookupResponse, error) {
	resp, err := s.scanUC.PHashLookup(ctx, req.Image)
	if err != nil {
		return nil, err
	}
	matches := make([]*scannerv1.PHashMatch, len(resp.Matches))
	for i, m := range resp.Matches {
		matches[i] = &scannerv1.PHashMatch{
			DocKey:     m.DocKey,
			Code:       m.Code,
			Rarity:     m.Rarity,
			Distance:   int32(m.Distance),
			Confidence: m.Confidence,
		}
	}
	return &scannerv1.PHashLookupResponse{
		Ok:       resp.OK,
		UserHash: resp.UserHash,
		Matches:  matches,
	}, nil
}

func scanResponseToProto(resp *scan.ScanResponse) *scannerv1.ScanResponse {
	out := &scannerv1.ScanResponse{
		Ok:           resp.OK,
		IdentifiedBy: resp.IdentifiedBy,
		Hash:         resp.Hash,
		PHash:        resp.PHash,
	}
	if resp.Card != nil {
		out.Card = &scannerv1.Card{
			Code:       resp.Card.Code,
			NameEn:     resp.Card.NameEn,
			NameJp:     resp.Card.NameJp,
			NameCn:     resp.Card.NameCn,
			Rarity:     resp.Card.Rarity,
			Type:       resp.Card.Type,
			Color:      resp.Card.Color,
			Promo:      resp.Card.Promo,
			Confidence: int32(resp.Card.Confidence),
			Lang:       resp.Card.Lang,
		}
	}
	return out
}

func safeString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
