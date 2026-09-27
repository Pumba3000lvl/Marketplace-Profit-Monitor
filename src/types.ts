import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type MarketplaceRoute = 'wb-tariffs' | 'wb-prices' | 'ozon' | 'metrics';
export type RequestMethod = 'GET' | 'POST';
export type MarketplaceSelection = 'wildberries' | 'ozon' | 'both';
export type MarketplaceQueryType = 'commissions' | 'prices' | 'profitability' | 'history';
export type MarketplaceCategoryId = `wb:${string}` | `ozon:${string}`;

export const MARKETPLACE_OPTIONS = [
  { label: 'Wildberries', value: 'wildberries' },
  { label: 'Ozon', value: 'ozon' },
  { label: 'Both', value: 'both' },
] as const;

export const QUERY_TYPE_OPTIONS = [
  { label: 'Commissions', value: 'commissions' },
  { label: 'Prices', value: 'prices' },
  { label: 'Profitability', value: 'profitability' },
  { label: 'History', value: 'history' },
] as const;

export interface MarketplaceCategoryOption {
  label: string;
  value: MarketplaceCategoryId;
}

export const MARKETPLACE_CATEGORY_OPTIONS: Record<MarketplaceSelection, MarketplaceCategoryOption[]> = {
  wildberries: [
    { label: 'Apparel and shoes', value: 'wb:apparel-and-shoes' },
    { label: 'Beauty and health', value: 'wb:beauty-and-health' },
    { label: 'Electronics', value: 'wb:electronics' },
    { label: 'Home and garden', value: 'wb:home-and-garden' },
    { label: 'Kids', value: 'wb:kids' },
  ],
  ozon: [
    { label: 'Apparel and shoes', value: 'ozon:apparel-and-shoes' },
    { label: 'Beauty and health', value: 'ozon:beauty-and-health' },
    { label: 'Electronics', value: 'ozon:electronics' },
    { label: 'Home and kitchen', value: 'ozon:home-and-kitchen' },
    { label: 'Kids', value: 'ozon:kids' },
  ],
  both: [
    { label: 'Wildberries — Apparel and shoes', value: 'wb:apparel-and-shoes' },
    { label: 'Wildberries — Beauty and health', value: 'wb:beauty-and-health' },
    { label: 'Wildberries — Electronics', value: 'wb:electronics' },
    { label: 'Wildberries — Home and garden', value: 'wb:home-and-garden' },
    { label: 'Wildberries — Kids', value: 'wb:kids' },
    { label: 'Ozon — Apparel and shoes', value: 'ozon:apparel-and-shoes' },
    { label: 'Ozon — Beauty and health', value: 'ozon:beauty-and-health' },
    { label: 'Ozon — Electronics', value: 'ozon:electronics' },
    { label: 'Ozon — Home and kitchen', value: 'ozon:home-and-kitchen' },
    { label: 'Ozon — Kids', value: 'ozon:kids' },
  ],
};

export const MAX_QUERY_LIMIT = 100_000;

export interface MarketplaceQuery extends DataQuery {
  marketplace: MarketplaceRoute;
  path: string;
  method: RequestMethod;
  body?: string;
  selectedMarketplace?: MarketplaceSelection;
  queryType?: MarketplaceQueryType;
  categories?: MarketplaceCategoryId[];
  limit?: number;
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
  selectedMarketplace: 'both',
  queryType: 'profitability',
  categories: [],
  limit: 1000,
};

export interface MarketplaceDataSourceOptions extends DataSourceJsonData {}

export interface MarketplaceSecureJsonData {
  wildberriesToken?: string;
  ozonClientId?: string;
  ozonApiKey?: string;
}
