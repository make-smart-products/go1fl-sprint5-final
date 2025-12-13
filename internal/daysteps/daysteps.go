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
		return errors.New("incorrect data format")
	}

	if parts[0] == "" || parts[1] == "" {
		return errors.New("empty step or duration value")
	}

	steps, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}

	if steps <= 0 {
		return errors.New("the number of steps must be greater than zero")
	}
	ds.Steps = steps

	duration, err := time.ParseDuration(parts[1])
	if err != nil {
		return err
	}

	if duration <= 0 {
		return errors.New("the duration must be greater than zero")
	}
	ds.Duration = duration

	return nil
}

func (ds DaySteps) ActionInfo() (string, error) {
	dist := spentenergy.Distance(ds.Steps, ds.Height)
	calories, err := spentenergy.WalkingSpentCalories(ds.Steps, ds.Weight, ds.Height, ds.Duration)
	if err != nil {
		return "", err
	}

	result := fmt.Sprintf("Количество шагов: %d.\nДистанция составила %.2f км.\nВы сожгли %.2f ккал.\n",
		ds.Steps,
		dist,
		calories,
	)

	return result, nil
}
