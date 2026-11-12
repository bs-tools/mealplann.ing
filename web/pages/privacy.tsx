import { cardClass } from "@components/ui/styles";
import type { ReactNode } from "react";
import React from "react";
import { Link } from "react-router";

/**
 * The address people write to about their data. Update the policy's
 * effective date whenever its content changes.
 */
const CONTACT_EMAIL = "johan.brandhorst@gmail.com";
const EFFECTIVE_DATE = "November 7, 2026";

interface SectionProps {
  title: string;
  children: ReactNode;
}

const Section: React.FC<SectionProps> = ({ title, children }) => (
  <section className="flex flex-col gap-2">
    <h2 className="text-lg font-semibold text-ink-soft">{title}</h2>
    {children}
  </section>
);

/**
 * PrivacyPage is the privacy policy. It's public, so people can read it
 * before they're invited.
 */
const PrivacyPage: React.FC = () => (
  <div className="flex justify-center py-4">
    <article
      className={`${cardClass} flex w-full max-w-3xl flex-col gap-6 p-6 text-sm leading-relaxed text-ink-soft sm:p-8`}
    >
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold text-ink-soft">Privacy policy</h1>
        <p className="text-muted">Effective {EFFECTIVE_DATE}</p>
      </header>

      <p>
        mealplann.ing is a small, invite-only web app for planning vegan meals
        for a household: recipes, weekly meal plans, the pantry and the shopping
        list, with nutrition checked against each person&apos;s targets. This
        policy explains what data it keeps about you, and what you can do about
        it. In short: your data is used only to run the app for your household.
        It isn&apos;t sold, used for advertising, or shared with anyone you
        haven&apos;t chosen to share it with.
      </p>

      <Section title="What we collect">
        <ul className="flex list-disc flex-col gap-1.5 pl-5">
          <li>
            <strong>Your account:</strong> your name, profile picture if you add
            one, and your passkeys&apos; public keys. We never see your
            passkeys&apos; private keys and don&apos;t use passwords.
          </li>
          <li>
            <strong>Your household:</strong> its name and who is in it.
          </li>
          <li>
            <strong>What you create:</strong> recipes, foods, meal plans, the
            pantry and shopping lists, and the eating patterns and nutrition
            targets of the people you plan for.
          </li>
          <li>
            <strong>Technical data:</strong> the server&apos;s logs, traces and
            error reports, which include IP addresses, the pages and API calls
            requested, and when.
          </li>
        </ul>
      </Section>

      <Section title="Health information">
        <p>
          Nutrition targets, portion limits, eating patterns and body
          measurements can reveal things about someone&apos;s health, like a
          medical condition. Only enter what you&apos;re comfortable storing,
          and only about people who agreed to it. The app uses this information
          only to check meal plans against it, and to recommend daily energy and
          macronutrients from a profile with standard formulas. It doesn&apos;t
          give medical advice: the targets are the ones you enter or choose to
          use.
        </p>
      </Section>

      <Section title="How we use it">
        <p>
          Only to provide the app&apos;s features to your household: keeping
          your recipes and plans, working out nutrition and shopping lists,
          suggesting recipes, and keeping the app secure and working. We
          don&apos;t profile you, show ads, or sell or rent your data.
        </p>
      </Section>

      <Section title="Who can see it">
        <p>
          Everyone in your household sees and can change the household&apos;s
          recipes, plans, pantry and shopping list, and the eating patterns and
          targets of the people in it. Nobody outside your household can see
          them.
        </p>
        <p>AI agents you connect can see your data too, as described below.</p>
      </Section>

      <Section title="AI transparency">
        <p>
          The app itself doesn&apos;t send your data to AI or machine learning
          services. You can choose to connect an AI agent, like Claude or
          ChatGPT, through the app&apos;s MCP server, so it can plan meals with
          you. Before an agent can access anything, you&apos;re asked to
          explicitly allow it on a consent page, which says that it will be able
          to see your household&apos;s data, including its health information.
        </p>
        <p>
          An agent you connect reads and changes your data on your behalf, as
          you would, and the company providing it handles that data under its
          own privacy policy. You can withdraw your consent at any time by
          disconnecting the agent under Connected apps in Settings, which stops
          its access immediately.
        </p>
      </Section>

      <Section title="Service providers">
        <p>We use these companies to run the app:</p>
        <ul className="flex list-disc flex-col gap-1.5 pl-5">
          <li>
            <strong>Google Cloud</strong>, in the United States, runs the server
            and stores profile pictures, logs, traces and error reports.
          </li>
          <li>
            <strong>Neon</strong> hosts the database.
          </li>
        </ul>
      </Section>

      <Section title="Cookies and storage on your device">
        <p>
          We use one cookie, to keep you signed in. The app also keeps unsaved
          drafts and a copy of itself for offline use on your device. There are
          no analytics or tracking cookies.
        </p>
      </Section>

      <Section title="How long we keep it, and deleting it">
        <p>
          We keep your data while you have an account. Logs, traces and error
          reports are deleted after 30 days.
        </p>
        <p>
          You can delete what you&apos;ve created at any time in the app. To
          delete your whole account or household, email us and we&apos;ll delete
          it within 30 days, along with the files stored for it.
        </p>
      </Section>

      <Section title="Your rights">
        <p>
          You can ask us for a copy of your data, to correct it, or to delete
          it, wherever you live. Email us and we&apos;ll answer within 30 days.
        </p>
      </Section>

      <Section title="Security">
        <p>
          Data is encrypted in transit and at rest. Sign in is with passkeys
          only.
        </p>
      </Section>

      <Section title="Children">
        <p>
          Accounts aren&apos;t meant for anyone under 16. A household can plan
          meals for children without them having an account.
        </p>
      </Section>

      <Section title="Changes">
        <p>
          We&apos;ll update this page, and its effective date, when this policy
          changes, and tell you in the app about changes that matter.
        </p>
      </Section>

      <Section title="Contact">
        <p>
          Write to{" "}
          <a
            href={`mailto:${CONTACT_EMAIL}`}
            className="font-medium text-pine hover:underline"
          >
            {CONTACT_EMAIL}
          </a>{" "}
          with any questions about your data.
        </p>
      </Section>

      <footer className="border-t border-stone-200 pt-4 text-muted">
        <Link to="/" className="font-medium text-pine hover:underline">
          Back to mealplann.ing
        </Link>
      </footer>
    </article>
  </div>
);

export default PrivacyPage;
