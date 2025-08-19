import { Component } from 'react';
import { useNavigate } from 'react-router-dom';

function ErrorFallback({ onRetry }) {
  const navigate = useNavigate();

  return (
    <div className="flex h-screen flex-col items-center justify-center gap-sm text-center">
      <h1 className="text-xl font-semibold">Something went wrong.</h1>
      <button
        onClick={() => {
          onRetry();
          navigate(0);
        }}
        className="rounded bg-primary px-md py-sm text-primary-foreground"
      >
        Retry
      </button>
    </div>
  );
}

export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError() {
    return { hasError: true };
  }

  componentDidCatch(error, errorInfo) {
    console.error('Error boundary caught an error', error, errorInfo);
  }

  handleRetry = () => {
    this.setState({ hasError: false });
  };

  render() {
    if (this.state.hasError) {
      return <ErrorFallback onRetry={this.handleRetry} />;
    }

    return this.props.children;
  }
}
