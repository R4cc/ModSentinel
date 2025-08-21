import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { describe, it, expect, vi } from 'vitest';

import Header from './Header.jsx';

describe('Header', () => {
  it('renders without instance switcher', () => {
    render(<Header onMenuClick={vi.fn()} />);
    expect(screen.queryByLabelText(/switch instance/i)).toBeNull();
    expect(screen.queryByText(/select instance/i)).toBeNull();
  });
});

