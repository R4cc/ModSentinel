import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { describe, it, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';

vi.mock('@/lib/api.ts', () => ({
  getToken: vi.fn(),
}));

import AlertsCard from './AlertsCard.jsx';

beforeEach(() => {
  sessionStorage.clear();
});

describe('AlertsCard', () => {
  it('shows no alerts when token exists and no error', async () => {
    const { getToken } = await import('@/lib/api.ts');
    getToken.mockResolvedValue('token');
    render(
      <MemoryRouter>
        <AlertsCard error='' onRetry={() => {}} />
      </MemoryRouter>
    );
    await screen.findByText('No alerts.');
  });

  it('shows token alert when token missing', async () => {
    const { getToken } = await import('@/lib/api.ts');
    getToken.mockResolvedValue(null);
    render(
      <MemoryRouter>
        <AlertsCard error='' onRetry={() => {}} />
      </MemoryRouter>
    );
    await screen.findByText('Modrinth token required.');
  });

  it('shows rate limit alert', async () => {
    const { getToken } = await import('@/lib/api.ts');
    getToken.mockResolvedValue('token');
    render(
      <MemoryRouter>
        <AlertsCard error='rate limited' onRetry={() => {}} />
      </MemoryRouter>
    );
    await screen.findByText('Rate limit hit.');
  });
});
