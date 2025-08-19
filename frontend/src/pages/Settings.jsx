import usePreferences from '@/hooks/usePreferences.js';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card.jsx';
import { Select } from '@/components/ui/Select.jsx';

export default function Settings() {
  const { theme, setTheme, interval, setInterval } = usePreferences();

  return (
    <div className="max-w-md space-y-lg">
      <Card>
        <CardHeader>
          <CardTitle>Preferences</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <div className="space-y-xs">
            <label htmlFor="theme" className="text-sm font-medium">
              Theme
            </label>
            <Select id="theme" value={theme} onChange={(e) => setTheme(e.target.value)}>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
              <option value="system">System</option>
            </Select>
          </div>
          <div className="space-y-xs">
            <label htmlFor="interval" className="text-sm font-medium">
              Update check
            </label>
            <Select
              id="interval"
              value={interval}
              onChange={(e) => setInterval(e.target.value)}
            >
              <option value="manual">Manual</option>
              <option value="hourly">Hourly</option>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
            </Select>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}