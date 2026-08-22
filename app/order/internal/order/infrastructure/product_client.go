package infrastructure

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	"github.com/eagle-go/eagle/app/order/internal/order/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type ProductClient struct {
	client productv1.ProductServiceClient
}

func NewProductClient(c *config.Upstream) (*ProductClient, func(), error) {
	conn, err := kratosgrpc.NewClient(
		context.Background(),
		kratosgrpc.WithEndpoint(c.GetProductEndpoint()),
		kratosgrpc.WithTimeout(c.GetTimeout().AsDuration()),
		kratosgrpc.WithMiddleware(tracing.Client()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect product service: %w", err)
	}
	return &ProductClient{client: productv1.NewProductServiceClient(conn)}, func() { _ = conn.Close() }, nil
}

func (c *ProductClient) BatchGet(ctx context.Context, ids []int64) ([]domain.ProductSnapshot, error) {
	resp, err := c.client.BatchGetProducts(ctx, &productv1.BatchGetProductsRequest{Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("batch get products: %w", err)
	}
	out := make([]domain.ProductSnapshot, 0, len(resp.GetProducts()))
	for _, product := range resp.GetProducts() {
		out = append(out, domain.ProductSnapshot{
			ID: product.GetId(), SKU: product.GetSku(), Name: product.GetName(),
			PriceCents: product.GetPriceCents(), Active: product.GetActive(),
		})
	}
	return out, nil
}
