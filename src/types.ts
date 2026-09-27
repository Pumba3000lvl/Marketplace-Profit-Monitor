import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type MarketplaceRoute = 'wb-tariffs' | 'wb-prices' | 'ozon' | 'metrics';
export type RequestMethod = 'GET' | 'POST';

export interface MarketplaceQuery extends DataQuery {
  marketplace: MarketplaceRoute;
  path: string;
  method: RequestMethod;
  body?: string;
}

export const MARKETPLACE_ROUTES: Array<{ label: string; value: MarketplaceRoute }> = [
  { label: 'Wildberries — common API', value: 'wb-tariffs' },
  { label: 'Wildberries — prices and discounts API', value: 'wb-prices' },
  { label: 'Ozon Seller API', value: 'ozon' },
  { label: 'All marketplaces — product metrics', value: 'metrics' },
];

export const DEFAULT_QUERY: Partial<MarketplaceQuery> = {
  marketplace: 'wb-tariffs',
  path: '/api/v1/tariffs/box',
  method: 'GET',
};

export interface MarketplaceDataSourceOptions extends DataSourceJsonData {}

export interface MarketplaceSecureJsonData {
  wildberriesToken?: string;
  ozonClientId?: string;
  ozonApiKey?: string;
}
