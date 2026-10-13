import { realApi, type StudioApi } from './client';
import { createMockApi } from './mock';
export const mockMode = import.meta.env.VITE_API_MODE === 'mock';
export const api: StudioApi = mockMode ? createMockApi() : realApi;
