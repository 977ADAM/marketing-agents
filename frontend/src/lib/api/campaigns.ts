// Запросы к API кампаний. Никакой логики: что пришло — то и показываем.

import { postJSON, request } from './client';
import type { Brief, Campaign, CampaignSummary, CreateRunResponse } from './types';

export function createCampaign(brief: Brief): Promise<CreateRunResponse> {
	return postJSON<CreateRunResponse>('campaigns', brief);
}

export function getCampaign(id: string): Promise<Campaign> {
	return request<Campaign>(`campaigns/${id}`);
}

export function listCampaigns(): Promise<CampaignSummary[]> {
	return request<CampaignSummary[]>('campaigns');
}
