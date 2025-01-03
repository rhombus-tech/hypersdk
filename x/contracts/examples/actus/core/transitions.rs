use borsh::{BorshDeserialize, BorshSerialize};
use wasmlanche::Units;

use super::types::*;
use super::Result;
use crate::math;

pub struct TransitionEngine;

impl TransitionEngine {
    pub fn process_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms
    ) -> Result<Option<Units>> {
        // Validate event timing
        if timestamp < state.status_date {
            return Err(Error::TransitionError("Event timestamp before status date".into()));
        }

        // Update accrued interest if time has passed
        if timestamp > state.status_date {
            Self::update_accrued_interest(state, terms, timestamp)?;
        }

        // Process event based on contract type
        let result = match terms.contract_type {
            ContractType::PAM => Self::process_pam_event(event, timestamp, state, terms),
            ContractType::LAM => Self::process_lam_event(event, timestamp, state, terms),
            _ => Err(Error::TransitionError("Unsupported contract type".into()))
        }?;

        // Update status date
        state.status_date = timestamp;

        Ok(result)
    }

    fn process_pam_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            },

            // Interest Payment
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let payment = state.accrued_interest;
                    state.accrued_interest = 0;
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            },

            // Principal Redemption at Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let principal = state.notional_principal;
                        let interest = state.accrued_interest;
                        let total_payment = principal + interest;

                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            },

            // Regular Monitoring/Accrual
            EventType::AD => {
                Self::update_accrued_interest(state, terms, timestamp)?;
                Ok(None)
            },

            // Rate Reset
            EventType::RR => {
                if let Some(rate) = terms.next_reset_rate {
                    state.nominal_interest_rate = rate;
                }
                Ok(None)
            },

            // Fee Payment
            EventType::FP => {
                if let Some(fee) = state.accrued_fees {
                    if fee > 0 {
                        let payment = fee;
                        state.accrued_fees = 0;
                        return Ok(Some(payment));
                    }
                }
                Ok(None)
            },

            // Principal Prepayment
            EventType::PP => {
                if let Some(pp_effect) = terms.prepayment_effect {
                    match pp_effect {
                        PrepaymentEffect::N => Ok(None),
                        PrepaymentEffect::A => {
                            // Reduce next principal payment
                            if let Some(amount) = state.next_principal_redemption_payment {
                                state.next_principal_redemption_payment = Some(0);
                                Ok(Some(amount))
                            } else {
                                Ok(None)
                            }
                        },
                        PrepaymentEffect::M => {
                            // Reduce maturity
                            let amount = state.notional_principal;
                            state.notional_principal = 0;
                            Ok(Some(amount))
                        }
                    }
                } else {
                    Ok(None)
                }
            },

            _ => Ok(None)
        }
    }

    fn process_lam_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms
    ) -> Result<Option<Units>> {
        // LAM specific transitions
        // TODO: Implement LAM transitions
        Ok(None)
    }

    fn update_accrued_interest(
        state: &mut ContractState,
        terms: &ContractTerms,
        timestamp: u64,
    ) -> Result<()> {
        if let Some(dcc) = terms.day_count_convention {
            let time_fraction = math::year_fraction(
                dcc,
                state.status_date,
                timestamp,
                terms.maturity_date
            );

            let accrual = state.notional_principal
                .checked_mul(state.nominal_interest_rate)
                .ok_or(Error::MathError("Interest calculation overflow".into()))?
                .checked_mul(time_fraction)
                .ok_or(Error::MathError("Interest calculation overflow".into()))?;

            state.accrued_interest = state.accrued_interest
                .checked_add(accrual)
                .ok_or(Error::MathError("Interest accrual overflow".into()))?;

            Ok(())
        } else {
            Ok(())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pam_initial_exchange() {
        let mut state = ContractState {
            status_date: 1000,
            notional_principal: 0,
            nominal_interest_rate: 0,
            accrued_interest: 0,
            contract_performance: ContractPerformance::PF,
        };

        let terms = ContractTerms {
            contract_type: ContractType::PAM,
            initial_exchange_date: Some(1000),
            notional_principal: Some(1000000),
            nominal_interest_rate: Some(50000), // 5% in basis points
            ..Default::default()
        };

        let result = TransitionEngine::process_event(
            EventType::IED,
            1000,
            &mut state,
            &terms
        ).unwrap();

        assert_eq!(result, Some(1000000));
        assert_eq!(state.notional_principal, 1000000);
        assert_eq!(state.nominal_interest_rate, 50000);
        assert_eq!(state.accrued_interest, 0);
    }

    // Add more tests for other transitions
}
