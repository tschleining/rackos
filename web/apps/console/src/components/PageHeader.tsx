
interface PageHeaderProps {
  title: string;
  description?: string;
}

export function PageHeader({
  title,
  description,
}: PageHeaderProps) {
  return (
    <header className="mb-8">
      <h1 className="text-2xl font-semibold tracking-tight text-rack-primary sm:text-3xl">
        {title}
      </h1>

      {description && (
        <p className="mt-2 text-sm leading-relaxed text-rack-muted">
          {description}
        </p>
      )}
    </header>
  );
}
