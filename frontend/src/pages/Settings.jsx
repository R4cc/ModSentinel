import { useEffect, useState } from "react";
import { toast } from "@/lib/toast.ts";
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
import { getSecretStatus, saveSecret, clearSecret, testPuffer, searchMods } from "@/lib/api.ts";

export default function Settings() {
  const { theme, setTheme, interval, setInterval } = usePreferences();
  const [token, setToken] = useState("");
  const [tokenLast4, setTokenLast4] = useState("");
  const [hasToken, setHasToken] = useState(false);
  const [show, setShow] = useState(false);
  const [baseUrl, setBaseUrl] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [deepScan, setDeepScan] = useState(false);
  const [scopes, setScopes] = useState(
    "server.view server.files.view server.files.edit",
  );
  const [pufferLast4, setPufferLast4] = useState("");
  const [hasPuffer, setHasPuffer] = useState(false);

  useEffect(() => {
    getSecretStatus("modrinth")
      .then((s) => {
        setHasToken(s.exists);
        setTokenLast4(s.last4);
      })
      .catch(() => {});
    getSecretStatus("pufferpanel")
      .then((s) => {
        setHasPuffer(s.exists);
        setPufferLast4(s.last4);
      })
      .catch(() => {});
  }, []);

  async function handleSave() {
    if (!token && hasToken) { toast.success("Token unchanged"); return; }
    if (!token) { toast.error("Token cannot be empty"); return; }
    try {
      await saveSecret("modrinth", { token });
      setToken("");
      setShow(false);
      const status = await getSecretStatus("modrinth");
      setHasToken(status.exists);
      setTokenLast4(status.last4);
      toast.success("Token saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save token");
    }
  }

  async function handleClear() {
    try {
      await clearSecret("modrinth");
      setHasToken(false);
      setTokenLast4("");
      toast.success("Token cleared");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to clear token");
    }
  }

  async function handlePufferSave() {
    if (!baseUrl) { toast.error("Base URL required"); return; }
    if (!hasPuffer && (!clientId || !clientSecret)) { toast.error("Client ID and Secret required"); return; }
    try {
      await saveSecret("pufferpanel", {
        base_url: baseUrl,
        client_id: clientId,
        client_secret: clientSecret,
        scopes: "server.view server.files.view server.files.edit",
        deep_scan: true,
      });
      setBaseUrl("");
      setClientId("");
      setClientSecret("");
      setShowSecret(false);
      const status = await getSecretStatus("pufferpanel");
      setHasPuffer(status.exists);
      setPufferLast4(status.last4);
      toast.success("Credentials saved");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to save credentials",
      );
    }
  }

  async function handlePufferClear() {
    try {
      await clearSecret("pufferpanel");
      setBaseUrl("");
      setClientId("");
      setClientSecret("");
      setPufferLast4("");
      setHasPuffer(false);
      toast.success("Credentials cleared");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to clear credentials",
      );
    }
  }

  async function handlePufferTest() {
    try {
      await testPuffer();
      toast.success("Connection ok");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to test connection",
      );
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
            {hasToken && (
              <p className="text-sm text-muted-foreground">
                Saved: ••••{tokenLast4}
              </p>
            )}
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
            <Button onClick={handleSave} disabled={!token}>
              Save
            </Button>
            <Button
              variant="secondary"
              onClick={handleClear}
              disabled={!hasToken}
            >
              Revoke & Clear
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            PufferPanel
            {hasPuffer && (
              <span className="ml-sm rounded bg-green-100 px-1.5 py-0.5 text-xs text-green-800">
                Configured
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <p className="text-sm text-muted-foreground">
            Base URL must include <code>http://</code> or <code>https://</code>
            with no trailing slash. Requires scopes
            <code>server.view</code>, <code>server.files.view</code>, and
            <code>server.files.edit</code>. Errors include a{" "}
            <code>requestId</code>
            for log correlation.
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
            {hasPuffer && (
              <p className="text-sm text-muted-foreground">
                Saved: ••••{pufferLast4}
              </p>
            )}
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
          <div className="space-y-xs">
            <label htmlFor="pp-scopes" className="text-sm font-medium">
              Scopes
            </label>
            <Input
              id="pp-scopes"
              value={scopes}
              onChange={(e) => setScopes(e.target.value)}
            />
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
            <Button
              type="button"
              variant="secondary"
              onClick={handlePufferTest}
              disabled={!hasPuffer}
            >
              Test Connection
            </Button>
            <Button
              onClick={handlePufferSave}
              disabled={!baseUrl || !clientId || !clientSecret}
            >
              Save
            </Button>
            <Button
              variant="secondary"
              onClick={handlePufferClear}
              disabled={!hasPuffer}
            >
              Revoke & Clear
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
