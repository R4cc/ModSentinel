import { useEffect, useState } from "react";
import { toast } from "sonner";
import usePreferences from "@/hooks/usePreferences.js";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card.jsx";
import { Select } from "@/components/ui/Select.jsx";
import { Input } from "@/components/ui/Input.jsx";
import { Button } from "@/components/ui/Button.jsx";
import { Checkbox } from "@/components/ui/Checkbox.jsx";
import {
  getToken,
  saveToken,
  clearToken,
  getPufferCreds,
  savePufferCreds,
  clearPufferCreds,
  testPufferCreds,
} from "@/lib/api.ts";

export default function Settings() {
  const { theme, setTheme, interval, setInterval } = usePreferences();
  const [token, setToken] = useState("");
  const [show, setShow] = useState(false);
  const [baseUrl, setBaseUrl] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [deepScan, setDeepScan] = useState(false);

  useEffect(() => {
    getToken()
      .then(setToken)
      .catch(() => {});
    getPufferCreds()
      .then((c) => {
        setBaseUrl(c.base_url || "");
        setClientId(c.client_id || "");
        setClientSecret(c.client_secret || "");
        setDeepScan(!!c.deep_scan);
      })
      .catch(() => {});
  }, []);

  async function handleSave() {
    if (!token) {
      toast.error("Token cannot be empty");
      return;
    }
    try {
      await saveToken(token);
      toast.success("Token saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save token");
    }
  }

  async function handleClear() {
    try {
      await clearToken();
      setToken("");
      toast.success("Token cleared");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to clear token");
    }
  }

  async function handlePufferSave() {
    if (!baseUrl || !clientId || !clientSecret) {
      toast.error("All fields required");
      return;
    }
    try {
      await savePufferCreds({
        base_url: baseUrl,
        client_id: clientId,
        client_secret: clientSecret,
        deep_scan: deepScan,
      });
      toast.success("Credentials saved");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to save credentials",
      );
    }
  }

  async function handlePufferClear() {
    try {
      await clearPufferCreds();
      setBaseUrl("");
      setClientId("");
      setClientSecret("");
      setDeepScan(false);
      toast.success("Credentials cleared");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to clear credentials",
      );
    }
  }

  async function handlePufferTest() {
    try {
      await testPufferCreds({
        base_url: baseUrl,
        client_id: clientId,
        client_secret: clientSecret,
        deep_scan: deepScan,
      });
      toast.success("Connection successful");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Connection failed");
    }
  }

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
            <Select
              id="theme"
              value={theme}
              onChange={(e) => setTheme(e.target.value)}
            >
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

      <Card>
        <CardHeader>
          <CardTitle>Modrinth API Token</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <div className="space-y-xs">
            <label htmlFor="token" className="text-sm font-medium">
              Token{" "}
              <a
                href="https://docs.modrinth.com/api/"
                target="_blank"
                rel="noreferrer"
                className="text-primary underline"
              >
                Docs
              </a>
            </label>
            <div className="flex gap-sm">
              <Input
                id="token"
                type={show ? "text" : "password"}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                className="flex-1"
              />
              <Button variant="secondary" onClick={() => setShow((s) => !s)}>
                {show ? "Hide" : "Show"}
              </Button>
            </div>
          </div>
          <div className="flex gap-sm">
            <Button onClick={handleSave}>Save</Button>
            <Button variant="secondary" onClick={handleClear} disabled={!token}>
              Clear
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>PufferPanel</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <p className="text-sm text-muted-foreground">
            Requires scopes <code>server.view</code> and
            <code> server.files.view</code>.
          </p>
          <div className="space-y-xs">
            <label htmlFor="pp-base" className="text-sm font-medium">
              Base URL
            </label>
            <Input
              id="pp-base"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
          </div>
          <div className="space-y-xs">
            <label htmlFor="pp-client" className="text-sm font-medium">
              Client ID
            </label>
            <Input
              id="pp-client"
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
            />
          </div>
          <div className="space-y-xs">
            <label htmlFor="pp-secret" className="text-sm font-medium">
              Client Secret
            </label>
            <div className="flex gap-sm">
              <Input
                id="pp-secret"
                type={showSecret ? "text" : "password"}
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                className="flex-1"
              />
              <Button
                variant="secondary"
                onClick={() => setShowSecret((s) => !s)}
              >
                {showSecret ? "Hide" : "Show"}
              </Button>
            </div>
          </div>
          <div className="flex items-center gap-sm">
            <Checkbox
              id="pp-deep"
              checked={deepScan}
              onChange={(e) => setDeepScan(e.target.checked)}
            />
            <label htmlFor="pp-deep" className="text-sm font-medium">
              Enable deep scan
            </label>
          </div>
          <div className="flex gap-sm">
            <Button onClick={handlePufferSave}>Save</Button>
            <Button
              variant="secondary"
              onClick={handlePufferClear}
              disabled={!baseUrl && !clientId && !clientSecret}
            >
              Clear
            </Button>
            <Button variant="secondary" onClick={handlePufferTest}>
              Test
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
