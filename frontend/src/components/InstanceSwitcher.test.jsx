import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { describe, it, expect, vi } from 'vitest';

const navigateMock = vi.fn();

vi.mock('@/lib/api.ts', () => ({
  getInstances: vi.fn(),
}));

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

import InstanceSwitcher from './InstanceSwitcher.jsx';
import { getInstances } from '@/lib/api.ts';
import { MemoryRouter } from 'react-router-dom';

describe('InstanceSwitcher', () => {
  it('navigates to selected instance', async () => {
    getInstances.mockResolvedValue([
      { id: 1, name: 'One' },
      { id: 2, name: 'Two' },
    ]);

    render(
      <MemoryRouter initialEntries={['/instances/1']}>
        <InstanceSwitcher />
      </MemoryRouter>
    );

    const select = await screen.findByLabelText('Switch instance');
    expect(select.parentElement).toHaveClass('w-full');
    expect(select.parentElement).toHaveClass('md:w-48');
    fireEvent.change(select, { target: { value: '2' } });
    expect(navigateMock).toHaveBeenCalledWith('/instances/2');
  });
});
