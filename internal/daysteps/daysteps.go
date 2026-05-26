package daysteps

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Yandex-Practicum/tracker/internal/personaldata"
	"github.com/Yandex-Practicum/tracker/internal/spentenergy"
)

type DaySteps struct {
	Steps    int
	Duration time.Duration
	personaldata.Personal
}

func (ds *DaySteps) Parse(datastring string) (err error) {
	parts := strings.Split(datastring, ",")
	if len(parts) != 2 {
		return errors.New("invalid day steps format")
	}

	steps, err := strconv.Atoi(parts[0])
	if err != nil || steps <= 0 {
		return errors.New("invalid steps")
	}

	duration, err := time.ParseDuration(parts[1])
	if err != nil || duration <= 0 {
		return errors.New("invalid duration")
	}

	ds.Steps = steps
	ds.Duration = duration
	return nil
}

func (ds DaySteps) ActionInfo() (string, error) {
	if ds.Steps <= 0 || ds.Duration <= 0 || ds.Weight <= 0 || ds.Height <= 0 {
		return "", errors.New("invalid day steps data")
	}

	calories, err := spentenergy.WalkingSpentCalories(ds.Steps, ds.Weight, ds.Height, ds.Duration)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"Количество шагов: %d.\nДистанция составила %.2f км.\nВы сожгли %.2f ккал.\n",
		ds.Steps,
		spentenergy.Distance(ds.Steps, ds.Height),
		calories,
	), nil
}
