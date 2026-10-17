@smoke
Feature: User login
  Background:
    Given a registered user

  Scenario: Successful login
    Given the user exists
    When the user logs in
    Then the dashboard is shown

  Scenario Outline: Invalid password
    When the user logs in with "<password>"
    Then an error is shown
    Examples:
      | password |
      | wrong    |
      | ""       |
